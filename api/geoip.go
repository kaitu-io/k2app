package center

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/netip"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gin-gonic/gin"
	db "github.com/wordgate/qtoolkit/db"
	"github.com/wordgate/qtoolkit/log"
)

// 目标国家代码（ISO 3166-1 alpha-2 小写），对应 feat/globalization task #16 的 14 个规则包
var geoipTargetCountries = []string{
	"cn", "ir", "ru", "tr", "pk", "vn", "mm",
	"eg", "id", "sa", "ae", "th", "bd", "by",
}

// profileByCountry 国家 → suggestedProfile 映射
var profileByCountry = map[string]string{
	"cn": "cnroute",
	"ir": "iroute",
	"ru": "ruroute",
	"tr": "troute",
	"pk": "pkroute",
	"vn": "vnroute",
	"mm": "mmroute",
	"eg": "egroute",
	"id": "idroute",
	"sa": "saroute",
	"ae": "aeroute",
	"th": "throute",
	"bd": "bdroute",
	"by": "byroute",
}

// SuggestedProfileForCountry 根据国家代码返回推荐的 routing profile 名称
// 未匹配或空值返回 "global"
func SuggestedProfileForCountry(cc string) string {
	if p, ok := profileByCountry[strings.ToLower(strings.TrimSpace(cc))]; ok {
		return p
	}
	return "global"
}

// geoipPrefixes 按 IPv4/IPv6 分组的前缀集合
// 为了支持 O(log n) 的二分查找，前缀按起始地址排序后存储
type geoipPrefixes struct {
	// v4 前缀的起始地址（big-endian 字节序），与 v4End/v4CC 下标对齐
	v4Start []netip.Addr
	v4End   []netip.Addr
	v4CC    []string

	v6Start []netip.Addr
	v6End   []netip.Addr
	v6CC    []string
}

var (
	geoipStore     atomic.Pointer[geoipPrefixes]
	geoipLoadOnce  sync.Once
	geoipLoader    geoipLoaderFunc = fetchLoyalsoldierPrefixes // 允许测试替换
	geoipLoaderMu  sync.Mutex
	geoipFetchHTTP = &http.Client{Timeout: 30 * time.Second}
)

// geoipLoaderFunc 抽象加载器签名，便于单元测试注入 stub 数据
type geoipLoaderFunc func(ctx context.Context, cc string) ([]netip.Prefix, error)

// SetGeoIPLoaderForTest 测试注入自定义加载器，返回清理函数
// 不得在生产代码中调用
func SetGeoIPLoaderForTest(loader geoipLoaderFunc) func() {
	geoipLoaderMu.Lock()
	defer geoipLoaderMu.Unlock()
	prev := geoipLoader
	geoipLoader = loader
	return func() {
		geoipLoaderMu.Lock()
		geoipLoader = prev
		geoipLoaderMu.Unlock()
	}
}

// InitGeoIP 在 Center 启动时执行一次加载，之后按 24h 刷新
// 单次国家加载失败不影响其他国家（WARN log + partial coverage）
func InitGeoIP(ctx context.Context) {
	geoipLoadOnce.Do(func() {
		if err := reloadGeoIP(ctx); err != nil {
			log.Warnf(ctx, "geoip initial load had errors: %v", err)
		}
		go geoipRefreshLoop()
	})
}

// ResetGeoIPForTest 清空 store 和 once gate，便于测试反复初始化
func ResetGeoIPForTest() {
	geoipStore.Store(nil)
	geoipLoadOnce = sync.Once{}
}

func geoipRefreshLoop() {
	ticker := time.NewTicker(24 * time.Hour)
	defer ticker.Stop()
	for range ticker.C {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
		if err := reloadGeoIP(ctx); err != nil {
			log.Warnf(ctx, "geoip refresh had errors: %v", err)
		}
		cancel()
	}
}

// reloadGeoIP 加载全部目标国家前缀并构建快速查找结构
// 返回的 error 仅用于日志聚合，部分失败不会导致整体失败
func reloadGeoIP(ctx context.Context) error {
	geoipLoaderMu.Lock()
	loader := geoipLoader
	geoipLoaderMu.Unlock()

	var (
		v4Entries []geoipEntry
		v6Entries []geoipEntry
		errs      []string
		total     int
	)
	for _, cc := range geoipTargetCountries {
		prefixes, err := loader(ctx, cc)
		if err != nil {
			errs = append(errs, fmt.Sprintf("%s: %v", cc, err))
			continue
		}
		for _, p := range prefixes {
			if !p.IsValid() {
				continue
			}
			entry := geoipEntry{prefix: p, cc: cc}
			if p.Addr().Is4() {
				v4Entries = append(v4Entries, entry)
			} else {
				v6Entries = append(v6Entries, entry)
			}
		}
		total += len(prefixes)
	}

	store := buildStore(v4Entries, v6Entries)
	geoipStore.Store(store)
	log.Infof(ctx, "geoip loaded: countries=%d v4=%d v6=%d total_prefixes=%d",
		len(geoipTargetCountries)-len(errs), len(store.v4Start), len(store.v6Start), total)

	if len(errs) > 0 {
		return fmt.Errorf("partial load errors: %s", strings.Join(errs, "; "))
	}
	return nil
}

type geoipEntry struct {
	prefix netip.Prefix
	cc     string
}

// buildStore 排序 + 展开为 start/end 数组以便二分查找
func buildStore(v4, v6 []geoipEntry) *geoipPrefixes {
	sortEntries := func(entries []geoipEntry) {
		sort.Slice(entries, func(i, j int) bool {
			return entries[i].prefix.Addr().Compare(entries[j].prefix.Addr()) < 0
		})
	}
	sortEntries(v4)
	sortEntries(v6)

	s := &geoipPrefixes{
		v4Start: make([]netip.Addr, len(v4)),
		v4End:   make([]netip.Addr, len(v4)),
		v4CC:    make([]string, len(v4)),
		v6Start: make([]netip.Addr, len(v6)),
		v6End:   make([]netip.Addr, len(v6)),
		v6CC:    make([]string, len(v6)),
	}
	for i, e := range v4 {
		s.v4Start[i] = e.prefix.Masked().Addr()
		s.v4End[i] = lastAddrOfPrefix(e.prefix)
		s.v4CC[i] = e.cc
	}
	for i, e := range v6 {
		s.v6Start[i] = e.prefix.Masked().Addr()
		s.v6End[i] = lastAddrOfPrefix(e.prefix)
		s.v6CC[i] = e.cc
	}
	return s
}

// lastAddrOfPrefix 计算 CIDR 的最后一个地址（含边界）
func lastAddrOfPrefix(p netip.Prefix) netip.Addr {
	bits := p.Bits()
	addr := p.Masked().Addr()
	if !addr.IsValid() {
		return addr
	}
	// 生成 host 位全 1 掩码
	buf := addr.As16()
	offset := 0
	if addr.Is4() {
		offset = 12 // IPv4-mapped 前缀位
	}
	totalBits := 128
	if addr.Is4() {
		totalBits = 32
	}
	for i := bits; i < totalBits; i++ {
		byteIdx := offset + i/8
		bitIdx := 7 - (i % 8)
		buf[byteIdx] |= 1 << bitIdx
	}
	if addr.Is4() {
		return netip.AddrFrom4([4]byte{buf[12], buf[13], buf[14], buf[15]})
	}
	return netip.AddrFrom16(buf)
}

// LookupCountry 返回 addr 所属目标国家的 ISO 小写代码
// 未匹配、私有地址、无效输入均返回 ""
func LookupCountry(addr netip.Addr) string {
	if !addr.IsValid() {
		return ""
	}
	// 先解包 4in6 映射地址
	if addr.Is4In6() {
		addr = addr.Unmap()
	}
	// 私有/回环/链路本地 直接忽略
	if addr.IsPrivate() || addr.IsLoopback() || addr.IsLinkLocalUnicast() ||
		addr.IsMulticast() || addr.IsUnspecified() {
		return ""
	}

	s := geoipStore.Load()
	if s == nil {
		return ""
	}

	var starts, ends []netip.Addr
	var ccs []string
	if addr.Is4() {
		starts, ends, ccs = s.v4Start, s.v4End, s.v4CC
	} else {
		starts, ends, ccs = s.v6Start, s.v6End, s.v6CC
	}
	if len(starts) == 0 {
		return ""
	}

	// 找到第一个 start > addr 的索引；候选区间是 idx-1
	idx := sort.Search(len(starts), func(i int) bool {
		return starts[i].Compare(addr) > 0
	})
	if idx == 0 {
		return ""
	}
	cand := idx - 1
	if addr.Compare(ends[cand]) <= 0 {
		return ccs[cand]
	}
	return ""
}

// CountryFromGinContext 从 Gin 请求上下文解析客户端公网 IP 并查表返回国家代码
// c.ClientIP() 已经处理 X-Forwarded-For / X-Real-IP，取决于 Gin 的 trusted proxy 配置
func CountryFromGinContext(c *gin.Context) string {
	if c == nil || c.Request == nil {
		return ""
	}
	ipStr := c.ClientIP()
	if ipStr == "" {
		return ""
	}
	addr, err := netip.ParseAddr(ipStr)
	if err != nil {
		return ""
	}
	return LookupCountry(addr)
}

// maybeUpdateUserCountry 在认证通过后异步更新 user.current_country（若检测到且已变化）
// 设计要点：
//   - 完全异步，不阻塞请求；单行按 PK 更新（sub-ms）
//   - 未检测到国家（私有 IP / 数据未加载 / 不在目标国家）直接 skip，不清空已有值
//   - 复制 user 指针快照，避免 goroutine 读到并发被修改的结构体
func maybeUpdateUserCountry(c *gin.Context, user *User) {
	if user == nil || user.ID == 0 {
		return
	}
	cc := CountryFromGinContext(c)
	if cc == "" {
		return
	}
	if cc == user.CurrentCountry {
		return
	}
	// 更新内存对象，让当前请求后续 handler 读到最新值
	user.CurrentCountry = cc
	userID := user.ID

	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		err := db.Get().WithContext(ctx).Model(&User{}).
			Where("id = ?", userID).
			Update("current_country", cc).Error
		if err != nil {
			log.Warnf(ctx, "failed to update current_country for user %d: %v", userID, err)
		}
	}()
}

// fetchLoyalsoldierPrefixes 从 Loyalsoldier/geoip text 仓库拉取指定国家的前缀列表
// 每行格式：CIDR（可能带注释）。忽略空行与 # 行
func fetchLoyalsoldierPrefixes(ctx context.Context, cc string) ([]netip.Prefix, error) {
	url := fmt.Sprintf("https://raw.githubusercontent.com/Loyalsoldier/geoip/release/text/%s.txt", cc)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	resp, err := geoipFetchHTTP.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		_, _ = io.Copy(io.Discard, resp.Body)
		return nil, fmt.Errorf("http %d", resp.StatusCode)
	}
	return parsePrefixStream(resp.Body)
}

// parsePrefixStream 解析文本 CIDR 列表
func parsePrefixStream(r io.Reader) ([]netip.Prefix, error) {
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 128*1024), 1024*1024)
	var out []netip.Prefix
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		// 支持 "1.0.0.0/24 # comment" 格式
		if idx := strings.IndexAny(line, " \t#"); idx >= 0 {
			line = strings.TrimSpace(line[:idx])
		}
		if line == "" {
			continue
		}
		p, err := netip.ParsePrefix(line)
		if err != nil {
			continue // 跳过 malformed 行
		}
		out = append(out, p)
	}
	if err := scanner.Err(); err != nil {
		return out, err
	}
	return out, nil
}
