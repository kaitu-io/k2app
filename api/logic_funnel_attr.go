package center

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"net"
	"sort"
	"strconv"
	"strings"
	"time"

	goredis "github.com/redis/go-redis/v9"
	"github.com/wordgate/qtoolkit/db"
	"github.com/wordgate/qtoolkit/log"
	"github.com/wordgate/qtoolkit/redis"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// 网页「点下载」→ App 首开 的推断归因（method = ip_os_v1）。
//
// 网页访客只有 sid，App 首开只有 did，两者之间没有确定性的联系。这里在 24h 窗口内按
// 「同品牌 + 同出口（IPv4 全地址 / IPv6 /64）+ 点的平台 == App 的系统」把两者配对：
//
//   - 点击侧（install_click）：Redis ZSET fattr:c:<HMAC(当日盐, brand|platform|ipKey)>，成员 sid、
//     分数 = 点击时刻；GPC / optout 访客没有 sid，不写。原始 IP 不落任何存储，盐每天换、49h 过期，
//     盐过期后哈希无法再还原成 IP。
//   - 首开侧（app_first_open）：只有发生后 5 分钟内送达的首开参与（迟到的首开可能已在隧道里，看到的
//     是节点 IP）；用今天和昨天的盐查窗口内的 sid，去重后恰好一个才写 funnel_inferred_links，
//     多于一个记为 ambiguous 丢弃——共享出口宁可漏配，不可误配。
//   - 推断关联绝不写 funnel_identities，只在管理端漏斗显式 ?inferred=1 时参与计算
//     （applyFunnelInferredLinks），确定性身份永远优先。
//
// 已知局限：ClientIP 取 X-Forwarded-For 最左项，可被客户端伪造（route.go 未配可信代理）——
// 伪造只能制造或破坏推断关联，推断数据只进报表、不授予任何东西。
const (
	funnelAttrMethod       = "ip_os_v1"
	funnelAttrWindow       = 24 * time.Hour
	funnelAttrMaxOpenDelay = 5 * time.Minute
	funnelAttrSaltTTL      = 49 * time.Hour
	funnelAttrClickTTL     = funnelAttrWindow + time.Hour
	funnelAttrMaxPerKey    = 50 // 单个出口哈希下最多保留的 sid 数（CGNAT 上界；超出丢最旧的）
	funnelAttrStatsTTL     = 130 * 24 * time.Hour
	funnelAttrRedisTimeout = 300 * time.Millisecond
	funnelAttrDayForm      = "20060102"
)

// 首开匹配的结果（计数器的字段名）。
const (
	funnelAttrMatched   = "matched"
	funnelAttrAmbiguous = "ambiguous"
	funnelAttrNone      = "none"
	funnelAttrLate      = "late"
	funnelAttrNoIP      = "noip"
)

var funnelAttrOutcomes = []string{funnelAttrMatched, funnelAttrAmbiguous, funnelAttrNone, funnelAttrLate, funnelAttrNoIP}

// funnelAttrPlatforms：install_click 的 p 与 App 上报的 os（normalizeFunnelOS 后）共用的取值。
var funnelAttrPlatforms = map[string]bool{"windows": true, "macos": true, "linux": true, "android": true, "ios": true}

// FunnelInferredLink：推断出的 did ↔ sid。与 funnel_identities 分表存放，永不混用。
type FunnelInferredLink struct {
	ID        uint64    `gorm:"primarykey"`
	CreatedAt time.Time `gorm:"index"` // 保留期按它删
	Brand     string    `gorm:"type:varchar(16);not null;uniqueIndex:uniq_inferred_did,priority:1"`
	DID       string    `gorm:"column:did;type:varchar(64);not null;uniqueIndex:uniq_inferred_did,priority:2"`
	SID       string    `gorm:"column:sid;type:varchar(64);not null;index"`
	Platform  string    `gorm:"type:varchar(16);not null"`
	Method    string    `gorm:"type:varchar(16);not null"`
	ClickAt   time.Time
	OpenAt    time.Time
}

func (FunnelInferredLink) TableName() string { return "funnel_inferred_links" }

// funnelAttrIPKey 把 ClientIP 归一成匹配用的出口键：IPv4（含 v4-mapped v6）取全地址，IPv6 取 /64
// （临时地址按 /128 会天天变）。非全局单播地址（回环 / 私网 / 链路本地 / 未指定）返回 ""。
func funnelAttrIPKey(raw string) string {
	ip := net.ParseIP(strings.TrimSpace(raw))
	if ip == nil || !ip.IsGlobalUnicast() || ip.IsPrivate() {
		return ""
	}
	if v4 := ip.To4(); v4 != nil {
		return "4:" + v4.String()
	}
	return "6:" + ip.Mask(net.CIDRMask(64, 128)).String()
}

func funnelAttrHash(salt string, brand Brand, platform, ipKey string) string {
	m := hmac.New(sha256.New, []byte(salt))
	m.Write([]byte(string(brand) + "|" + platform + "|" + ipKey))
	return hex.EncodeToString(m.Sum(nil))[:32]
}

func funnelAttrDay(t time.Time) string { return t.UTC().Format(funnelAttrDayForm) }

// funnelAttrStore 包一层 Redis，便于测试注入独立实例。
type funnelAttrStore struct{ rdb goredis.Cmdable }

func defaultFunnelAttrStore() funnelAttrStore { return funnelAttrStore{rdb: redis.Client()} }

// salt 取某天的盐；create=true 时不存在就生成（SETNX，两台 center 共用同一份）。
// create=false 且不存在时返回 ""（那天没有任何点击，不必生成）。
func (s funnelAttrStore) salt(ctx context.Context, day string, create bool) (string, error) {
	key := "fattr:salt:" + day
	if create {
		buf := make([]byte, 32)
		if _, err := rand.Read(buf); err != nil {
			return "", err
		}
		if err := s.rdb.SetNX(ctx, key, hex.EncodeToString(buf), funnelAttrSaltTTL).Err(); err != nil {
			return "", err
		}
	}
	v, err := s.rdb.Get(ctx, key).Result()
	if err == goredis.Nil {
		return "", nil
	}
	return v, err
}

// recordClick 记一次 install_click。sid 为空（GPC / optout）、平台未知、出口无效时什么都不写。
func (s funnelAttrStore) recordClick(ctx context.Context, brand Brand, platform, clientIP, sid string, now time.Time) error {
	if sid == "" || !funnelAttrPlatforms[platform] {
		return nil
	}
	ipKey := funnelAttrIPKey(clientIP)
	if ipKey == "" {
		return nil
	}
	salt, err := s.salt(ctx, funnelAttrDay(now), true)
	if err != nil || salt == "" {
		return err
	}
	key := "fattr:c:" + funnelAttrHash(salt, brand, platform, ipKey)
	_, err = s.rdb.TxPipelined(ctx, func(p goredis.Pipeliner) error {
		p.ZAdd(ctx, key, goredis.Z{Score: float64(now.Unix()), Member: sid})
		p.ZRemRangeByScore(ctx, key, "-inf", "("+strconv.FormatInt(now.Add(-funnelAttrWindow).Unix(), 10))
		p.ZRemRangeByRank(ctx, key, 0, -(funnelAttrMaxPerKey + 1))
		p.Expire(ctx, key, funnelAttrClickTTL)
		return nil
	})
	return err
}

// matchOpen 为一次首开找窗口 [now-24h, now] 内同出口、同平台的点击。返回去重后的结果：
// 恰好一个 sid → matched（附该 sid 最近一次点击时刻）；多个 → ambiguous；没有 → none。
func (s funnelAttrStore) matchOpen(ctx context.Context, brand Brand, platform, clientIP string, now time.Time) (sid string, clickAt time.Time, outcome string, err error) {
	ipKey := funnelAttrIPKey(clientIP)
	if ipKey == "" || !funnelAttrPlatforms[platform] {
		return "", time.Time{}, funnelAttrNoIP, nil
	}
	latest := map[string]int64{}
	lo := strconv.FormatInt(now.Add(-funnelAttrWindow).Unix(), 10)
	hi := strconv.FormatInt(now.Unix(), 10)
	for _, day := range []time.Time{now, now.Add(-funnelDay)} {
		salt, err := s.salt(ctx, funnelAttrDay(day), false)
		if err != nil {
			return "", time.Time{}, "", err
		}
		if salt == "" {
			continue
		}
		zs, err := s.rdb.ZRangeByScoreWithScores(ctx, "fattr:c:"+funnelAttrHash(salt, brand, platform, ipKey),
			&goredis.ZRangeBy{Min: lo, Max: hi}).Result()
		if err != nil {
			return "", time.Time{}, "", err
		}
		for _, z := range zs {
			m, _ := z.Member.(string)
			if at := int64(z.Score); m != "" && at > latest[m] {
				latest[m] = at
			}
		}
	}
	switch len(latest) {
	case 0:
		return "", time.Time{}, funnelAttrNone, nil
	case 1:
		for m, at := range latest {
			return m, time.Unix(at, 0), funnelAttrMatched, nil
		}
	}
	return "", time.Time{}, funnelAttrAmbiguous, nil
}

func (s funnelAttrStore) count(ctx context.Context, brand Brand, outcome string, now time.Time) {
	key := "fattr:stats:" + string(brand) + ":" + funnelAttrDay(now)
	_, _ = s.rdb.TxPipelined(ctx, func(p goredis.Pipeliner) error {
		p.HIncrBy(ctx, key, outcome, 1)
		p.Expire(ctx, key, funnelAttrStatsTTL)
		return nil
	})
}

// funnelAttrOpenOnTime：首开须在发生后 funnelAttrMaxOpenDelay 内送达（容忍同样幅度的客户端时钟超前）。
// 缺 created_at 视为迟到——无法证明这次上报走的是连 VPN 之前的出口。
func funnelAttrOpenOnTime(createdAt, now time.Time) bool {
	if createdAt.IsZero() {
		return false
	}
	d := now.Sub(createdAt)
	return d <= funnelAttrMaxOpenDelay && d >= -funnelAttrMaxOpenDelay
}

// onFirstOpen 处理一次合法的 app_first_open：判定、计数、匹配上则写推断关联（插入幂等）。
// 返回写入（或本应写入）的关联；未匹配返回 nil。
func (s funnelAttrStore) onFirstOpen(ctx context.Context, brand Brand, platform, clientIP, did string, createdAt, now time.Time) (*FunnelInferredLink, string, error) {
	if !funnelAttrOpenOnTime(createdAt, now) {
		s.count(ctx, brand, funnelAttrLate, now)
		return nil, funnelAttrLate, nil
	}
	sid, clickAt, outcome, err := s.matchOpen(ctx, brand, platform, clientIP, now)
	if err != nil {
		return nil, "", err
	}
	s.count(ctx, brand, outcome, now)
	if outcome != funnelAttrMatched {
		return nil, outcome, nil
	}
	return &FunnelInferredLink{
		Brand: string(brand), DID: did, SID: sid, Platform: platform, Method: funnelAttrMethod,
		ClickAt: clickAt, OpenAt: now,
	}, outcome, nil
}

// funnelAttrRecordClick：px 入口调用。失败只记日志，绝不影响像素响应。
func funnelAttrRecordClick(ctx context.Context, brand Brand, platform, clientIP, sid string) {
	if !funnelEnabled() || sid == "" {
		return
	}
	cctx, cancel := context.WithTimeout(context.Background(), funnelAttrRedisTimeout)
	defer cancel()
	if err := defaultFunnelAttrStore().recordClick(cctx, brand, platform, clientIP, sid, time.Now()); err != nil {
		log.Warnf(ctx, "[FunnelAttr] record click failed: %v", err)
	}
}

// funnelAttrFirstOpen：stats 入口对每条合法 app_first_open 调用。匹配上则异步插入（重复首开 / 重发
// 撞唯一键直接忽略）。失败只记日志，绝不影响上报响应。
func funnelAttrFirstOpen(ctx context.Context, brand Brand, platform, clientIP, did string, createdAt time.Time) {
	if !funnelEnabled() || did == "" {
		return
	}
	cctx, cancel := context.WithTimeout(context.Background(), funnelAttrRedisTimeout)
	defer cancel()
	link, outcome, err := defaultFunnelAttrStore().onFirstOpen(cctx, brand, platform, clientIP, did, createdAt, time.Now())
	if err != nil {
		log.Warnf(ctx, "[FunnelAttr] first_open match failed: %v", err)
		return
	}
	log.Infof(ctx, "[FunnelAttr] first_open brand=%s platform=%s outcome=%s", brand, platform, outcome)
	if link == nil {
		return
	}
	go func() {
		if err := db.Get().Clauses(clause.OnConflict{DoNothing: true}).Create(link).Error; err != nil {
			log.Warnf(context.Background(), "[FunnelAttr] insert inferred link failed: %v", err)
		}
	}()
}

// loadFunnelAttrStats 汇总 [from, to) 内各 UTC 日的首开匹配计数（brand 为空 = 全部品牌）。
func loadFunnelAttrStats(ctx context.Context, brand Brand, hasBrand bool, from, to time.Time) map[string]int64 {
	out := make(map[string]int64, len(funnelAttrOutcomes))
	for _, o := range funnelAttrOutcomes {
		out[o] = 0
	}
	brands := []Brand{brand}
	if !hasBrand {
		brands = []Brand{BrandKaitu, BrandOverleap}
	}
	rdb := redis.Client()
	for d := funnelUTCDay(from); d.Before(to); d = d.Add(funnelDay) {
		for _, b := range brands {
			vals, err := rdb.HGetAll(ctx, "fattr:stats:"+string(b)+":"+funnelAttrDay(d)).Result()
			if err != nil {
				continue
			}
			for k, v := range vals {
				if n, err := strconv.ParseInt(v, 10, 64); err == nil {
					if _, known := out[k]; known {
						out[k] += n
					}
				}
			}
		}
	}
	return out
}

// FunnelInferredSummary 随 ?inferred=1 的漏斗结果返回。
//   - Links：本次计算涉及的推断关联数
//   - Applied：实际改变了归属的关联数（确定性身份没有覆盖它）
//   - Confirmed：两端各自确定性关联到同一用户（推断被证实）
//   - Conflicts：两端各自确定性关联到不同用户（推断被证伪，已忽略）——Conflicts/(Conflicts+Confirmed) 即实测误配率
//   - Outcomes：区间内每天首开匹配结果计数（matched / ambiguous / none / late / noip）
type FunnelInferredSummary struct {
	Method    string           `json:"method"`
	Links     int              `json:"links"`
	Applied   int              `json:"applied"`
	Confirmed int              `json:"confirmed"`
	Conflicts int              `json:"conflicts"`
	Outcomes  map[string]int64 `json:"outcomes"`
}

// applyFunnelInferredLinks 把推断关联并入人的归集，纯函数，不修改输入。
// links 的键是 did、值是 sid（同一 did 只能有一个 sid）。对每条关联：
//
//   - did、sid 都已确定性关联到用户：同一用户 → Confirmed；不同 → Conflicts，忽略。
//   - 只有一端有用户：另一端并到该用户（若该端已被更早处理的关联并到别的用户 → Conflicts）。
//   - 都没有：该 did 的匿名记录改记到 sid 名下（同一个人）。
//
// 返回新的记录切片（只在需要改写时复制）、新的身份映射和摘要。
func applyFunnelInferredLinks(recs []funnelRecord, identities map[string]uint64, links map[string]string) ([]funnelRecord, map[string]uint64, FunnelInferredSummary) {
	sum := FunnelInferredSummary{Method: funnelAttrMethod, Links: len(links)}
	ids := make(map[string]uint64, len(identities)+len(links))
	for k, v := range identities {
		ids[k] = v
	}
	dids := make([]string, 0, len(links))
	for d := range links {
		dids = append(dids, d)
	}
	sort.Strings(dids) // 处理顺序确定 → 结果不依赖 map 迭代顺序

	alias := map[string]string{} // did → sid：两端都没有用户时改写记录
	for _, d := range dids {
		s := links[d]
		dk, sk := "did:"+d, "sid:"+s
		uD, uS := identities[dk], identities[sk] // 确定性身份用原始映射判断
		switch {
		case uD != 0 && uS != 0:
			if uD == uS {
				sum.Confirmed++
			} else {
				sum.Conflicts++
			}
		case uD != 0:
			if cur, ok := ids[sk]; ok && cur != uD {
				sum.Conflicts++
				continue
			}
			ids[sk] = uD
			sum.Applied++
		case uS != 0:
			ids[dk] = uS
			sum.Applied++
		default:
			if cur, ok := ids[sk]; ok { // sid 已被前面的关联并到某用户 → did 跟过去
				ids[dk] = cur
			} else {
				alias[d] = s
			}
			sum.Applied++
		}
	}
	if len(alias) == 0 {
		return recs, ids, sum
	}
	out := make([]funnelRecord, len(recs))
	copy(out, recs)
	for i := range out {
		r := &out[i]
		if r.AnonKind != "did" {
			continue
		}
		if s, ok := alias[r.AnonID]; ok {
			if _, linked := ids["did:"+r.AnonID]; !linked {
				r.AnonKind, r.AnonID = "sid", s
			}
		}
	}
	return out, ids, sum
}

// loadFunnelInferredLinks 取 recs 里出现的 did / sid 涉及的推断关联（brand 过滤与事件一致）。
// 同一 did 出现在多个品牌时取最早的一条。
func loadFunnelInferredLinks(ctx context.Context, brand Brand, hasBrand bool, recs []funnelRecord) (map[string]string, error) {
	didSet, sidSet := map[string]bool{}, map[string]bool{}
	for i := range recs {
		switch recs[i].AnonKind {
		case "did":
			didSet[recs[i].AnonID] = true
		case "sid":
			sidSet[recs[i].AnonID] = true
		}
	}
	out := map[string]string{}
	load := func(col string, set map[string]bool) error {
		vals := make([]string, 0, len(set))
		for v := range set {
			if v != "" {
				vals = append(vals, v)
			}
		}
		sort.Strings(vals)
		for start := 0; start < len(vals); start += funnelLoadBatch {
			end := min(start+funnelLoadBatch, len(vals))
			var rows []FunnelInferredLink
			q := db.Get().WithContext(ctx).Model(&FunnelInferredLink{}).Where(col+" IN ?", vals[start:end])
			if hasBrand {
				q = q.Where("brand = ?", string(brand))
			}
			if err := q.Order("created_at ASC, id ASC").Find(&rows).Error; err != nil {
				return err
			}
			for i := range rows {
				if _, ok := out[rows[i].DID]; !ok {
					out[rows[i].DID] = rows[i].SID
				}
			}
		}
		return nil
	}
	if err := load("did", didSet); err != nil {
		return nil, err
	}
	if err := load("sid", sidSet); err != nil {
		return nil, err
	}
	return out, nil
}

// funnelInferredEndpointRecs：给 loadFunnelIdentities 用的合成记录，让关联两端的身份都被查到
// （关联的另一端可能不在本次装载的事件里）。
func funnelInferredEndpointRecs(links map[string]string) []funnelRecord {
	out := make([]funnelRecord, 0, 2*len(links))
	for d, s := range links {
		out = append(out, funnelRecord{AnonKind: "did", AnonID: d}, funnelRecord{AnonKind: "sid", AnonID: s})
	}
	return out
}

// deleteFunnelInferredLinksForUsers：删号时删掉经该用户确定性身份能定位到的推断关联。
// 必须在删 funnel_identities 之前调用（靠它定位 sid / did）。
func deleteFunnelInferredLinksForUsers(tx *gorm.DB, userIDs []uint64) error {
	if len(userIDs) == 0 {
		return nil
	}
	dids := tx.Model(&FunnelIdentity{}).Select("anon_id").Where("kind = ? AND user_id IN ?", "did", userIDs)
	sids := tx.Model(&FunnelIdentity{}).Select("anon_id").Where("kind = ? AND user_id IN ?", "sid", userIDs)
	return tx.Where("did IN (?) OR sid IN (?)", dids, sids).Delete(&FunnelInferredLink{}).Error
}
