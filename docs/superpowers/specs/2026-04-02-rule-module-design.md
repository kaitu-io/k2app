# k2 Rule Module Design Spec

> 替代 k2rule，实现基于 routes 的多规则集路由引擎

## 问题

k2rule v0.6.4 的单文件模型依赖 DNS pipeline 的 `SetTmpRule` 来标记 IP。当 app（WhatsApp、Telegram 等）使用硬编码 IP 或私有 DNS 协议绕过 k2 的 DNS 时，`SetTmpRule` 不会触发，IP 被错误地路由为 DIRECT。

业界所有主流工具（Clash/Mihomo、sing-box、Surge、v2ray）通过维护**按服务分类的 IP-CIDR 列表**解决此问题。k2 需要同样的能力。

## 配置格式

统一使用 `routes` 路由表。每条 route = 匹配规则 + 出口（`via`）。First-match-wins。

### 基本用法（翻出去）

```yaml
routes:
  - via: "k2v5://us-server:443?ech=..."
    match:
      preset: overseas
  - via: direct
    match:
      names: [geoip-cn, cn-sites]
  - via: direct
```

### 翻回来

```yaml
routes:
  - via: "k2v5://cn-server:443?ech=..."
    match:
      names: [cn-sites, bilibili, geoip-cn]
  - via: direct
```

### 多 server 分流（Phase 2）

```yaml
routes:
  - via: "k2v5://us-server:443?ech=..."
    match:
      preset: overseas
  - via: "k2v5://cn-server:443?ech=..."
    match:
      names: [cn-sites, geoip-cn]
  - via: direct
```

### 全局代理

```yaml
routes:
  - via: "k2v5://us-server:443?ech=..."
    match: { all: true }
```

### 自定义覆盖

```yaml
routes:
  - via: "k2v5://us-server:443?ech=..."
    match:
      preset: overseas
      exclude: [openai]
  - via: reject
    match:
      names: [openai]
  - via: direct
```

### 语义

- `via`: k2v5:// URL（走服务器）或 `direct` / `reject`（关键字）
- `match.preset`: 预设规则集组（`overseas`、`cn-access`）
- `match.names`: 显式规则集名称列表
- `match.exclude`: 从 preset 中排除的规则集
- `match.all`: 匹配所有流量（全局代理）
- 最后一条 route 的 match 可为空或 `{}`，作为兜底
- Phase 1 限制：只支持一个 k2v5:// server，多 server 报错

### 配置类型

```go
type ClientConfig struct {
    Routes  []RouteConfig `yaml:"routes"  json:"routes"`
    Mode    string        `yaml:"mode"    json:"mode"`
    Tun     TunConfig     `yaml:"tun"     json:"tun"`
    Proxy   ProxyConfig   `yaml:"proxy"   json:"proxy"`
    DNS     DNSConfig     `yaml:"dns"     json:"dns"`
    Log     LogConfig     `yaml:"log"     json:"log"`
    Listen  string        `yaml:"listen"  json:"listen"`
    PIDFile string        `yaml:"pid_file" json:"pid_file"`
    BlockQUIC bool        `yaml:"block_quic" json:"block_quic"`
}

type RouteConfig struct {
    Via   string      `yaml:"via"   json:"via"`
    Match MatchConfig `yaml:"match" json:"match"`
}

type MatchConfig struct {
    Preset  string   `yaml:"preset,omitempty"  json:"preset,omitempty"`
    Names   []string `yaml:"names,omitempty"   json:"names,omitempty"`
    Exclude []string `yaml:"exclude,omitempty" json:"exclude,omitempty"`
    All     bool     `yaml:"all,omitempty"     json:"all,omitempty"`
}
```

`Server` 和 `Rule` 字段删除。engine 内部从 routes 中提取第一个 k2v5:// URL 作为 wire 连接地址。

## 两阶段匹配模型

```
阶段 1（DNS 时）: Match(domain) → route index
                  → SetTmpRule(learned IPs, route index)

阶段 2（连接时）: Match(destIP)
                  → TmpRule（DNS 动态学习）     最高优先级
                  → 按 route 顺序遍历规则集     静态规则
                  → 最后一条 route（兜底）
```

### Route 到 Target 的映射

Phase 1 只有一个 server，所有 `via: k2v5://...` 映射到 `TargetProxy`，`via: direct` 映射到 `TargetDirect`，`via: reject` 映射到 `TargetReject`。Tunnel 层不需要改动。

Phase 2 扩展 Target 为 route index，tunnel 根据 index 选择 dialer。

### SNI 嗅探

`match: { all: true }` 时 sniffBT=false, sniffSNI=false（全局代理不需要区分）。
其他情况 sniffBT=true, sniffSNI=true。

## 规则集 = 纯匹配数据（headless）

规则集文件只包含"这些域名和 IP 属于某个服务"，不包含 action。action 由 routes 中的 via 决定。

### 规则集按公司/基础设施组织

| 规则集 | 域名覆盖 | IP-CIDR 覆盖 |
|--------|---------|-------------|
| google | google.com, youtube.com, googleapis.com, gstatic.com, ... | Google 官方 IP 段 |
| facebook | facebook.com, whatsapp.com, instagram.com, messenger.com, ... | Meta ASN IP 段 |
| telegram | telegram.org, t.me, ... | Telegram 官方 CIDR |
| twitter | twitter.com, x.com, ... | Twitter/X ASN IP 段 |
| netflix | netflix.com, nflxvideo.net, ... | Netflix ASN IP 段 |
| cloudflare | — | Cloudflare 官方 IP 段 |
| discord | discord.com, discordapp.com, ... | 无独立 IP |
| spotify | spotify.com, ... | 无独立 IP |
| tiktok | tiktok.com, ... | 无独立 IP |
| openai | openai.com, chatgpt.com, ... | 无独立 IP |
| github | github.com, githubusercontent.com, ... | 无独立 IP |
| geoip-cn | — | 中国 IP 段（APNIC CN 分配） |
| cn-sites | 中国常用网站域名 | — |

## 数据源与许可证

| 数据源 | 许可证 | 用途 |
|--------|--------|------|
| v2fly/domain-list-community | MIT | 域名列表 |
| Loyalsoldier/geoip | CC-BY-SA-4.0 | 按服务 IP-CIDR |
| 官方 IP 发布 (Google/Telegram/CF) | 公开数据 | IP 段 |
| go4.org/netipx | BSD-3-Clause | 编译器用 |

不可用：sing/common/domain (GPL-3.0)、SagerNet 编译产物 (GPL-3.0)、metacubex 编译产物 (GPL-3.0)。

## 文件格式：k2b v1（mmap-indexed）

设计目标：mmap 后零解析、零 Go heap 分配、二分查找直接在文件页上操作。

### 总览

```
┌─ File Header (32 bytes) ─────────────────────────────┐
│ Magic:       "K2RB"    (4 bytes)                      │
│ Version:     uint32 LE (= 1)                          │
│ SetCount:    uint32 LE                                │
│ Created:     uint64 LE (unix timestamp)               │
│ Checksum:    uint64 LE (xxhash64, Header 之后所有数据) │
│ Reserved:    4 bytes                                  │
├─ Set Index (28 bytes x SetCount) ────────────────────┤
│ NameOffset:     uint32 LE                             │
│ NameLen:        uint16 LE                             │
│ _reserved:      uint16 LE                             │
│ DomainOffset:   uint32 LE                             │
│ DomainCount:    uint32 LE                             │
│ IPv4Offset:     uint32 LE                             │
│ IPv4Count:      uint16 LE                             │
│ IPv6Offset:     uint32 LE                             │
│ IPv6Count:      uint16 LE                             │
├─ Data ───────────────────────────────────────────────┤
│ Names Section     (连续 UTF-8 字符串)                  │
│ Domain Sections   (每个 RuleSet 一段)                  │
│ IPv4 Sections     (每个 RuleSet 一段)                  │
│ IPv6 Sections     (每个 RuleSet 一段)                  │
└──────────────────────────────────────────────────────┘
```

### 域名区布局（复用 k2rule v3 算法）

```
Offsets[Count+1]: uint32 LE[]     字符串偏移量数组（含哨兵）
Strings area:                     反转 + 小写 + dot 前缀的域名，排序

存储: ".youtube.com" → 反转 → "moc.ebutuoy."
查询: "www.youtube.com"
  → 生成后缀: ".www.youtube.com", ".youtube.com", ".com"
  → 逐个反转后二分查找
  → 命中 "moc.ebutuoy." → 匹配

所有条目均为 suffix 匹配（存 ".google.com" 匹配 google.com 及 *.google.com）
```

### IPv4 区布局（改进：排序 Range 对 + 二分查找）

```
Ranges[Count]: (from[4] + to[4])[]    已排序、已合并的 IPv4 范围

每条 8 bytes:
  [08 08 08 00] [08 08 08 FF]  → 8.8.8.0 ~ 8.8.8.255

匹配: 二分查找 from <= target 的最大条目，验证 target <= to
复杂度: O(log n)，零分配
```

### IPv6 区布局

```
Ranges[Count]: (from[16] + to[16])[]  每条 32 bytes，同 IPv4 逻辑
```

### 体积预估

| 内容 | 磁盘 | 下载（gzip） | 内存峰值 |
|------|------|------------|---------|
| overseas (15 服务) | ~200KB | ~60KB | ~16KB |
| cn-direct | ~130KB | ~35KB | ~12KB |
| 典型用户总计 | ~330KB | ~95KB | ~20KB |

### 分发

```
下载: HTTP GET overseas.k2b.gz (~60KB) → 解压写入 cache_dir/overseas.k2b → mmap
```

## rule 包设计

### 包结构

```
rule/
├── target.go       Target 类型, SetConfig, 预设展开, 合并
├── domain.go       域名二分查找 (k2rule v3 算法)
├── ip.go           IP Range 二分查找
├── format.go       k2b v1 reader/writer
├── engine.go       Engine: Match, SetTmpRule, Close
├── loader.go       Load: 从 cache_dir 读取 .k2b 文件
└── *_test.go       各组件测试
```

### 核心类型

```go
type Target uint8

const (
    TargetDirect Target = iota
    TargetProxy
    TargetReject
)

type BundleSet struct {
    Name    string
    domains domainSection
    ipv4    ipv4Section
    ipv6    ipv6Section
}

type BoundSet struct {
    Set    *BundleSet
    Target Target
}

type Engine struct {
    mu       deadlock.RWMutex
    tmp      map[netip.Addr]Target
    routes   []routeEntry          // 按 route 顺序排列的绑定规则集
    fallback Target
    bundles  []*Bundle
}

type routeEntry struct {
    sets   []BoundSet // 这条 route 的所有规则集
    target Target     // 命中时的 action
}
```

### Match 算法

```go
func (e *Engine) Match(host string) Target {
    e.mu.RLock()
    defer e.mu.RUnlock()

    addr, err := netip.ParseAddr(host)
    if err == nil {
        // IP path
        if t, ok := e.tmp[addr]; ok {
            return t
        }
        for i := range e.routes {
            for j := range e.routes[i].sets {
                if e.routes[i].sets[j].Set.MatchIP(addr) {
                    return e.routes[i].target
                }
            }
        }
    } else {
        // Domain path
        domain := strings.ToLower(host)
        for i := range e.routes {
            for j := range e.routes[i].sets {
                if e.routes[i].sets[j].Set.MatchDomain(domain) {
                    return e.routes[i].target
                }
            }
        }
    }
    return e.fallback
}
```

### 从 routes 构建 Engine

```go
// engine/engine.go 中
func buildRuleEngine(routes []config.RouteConfig, cacheDir string) *rule.Engine {
    bundles, _ := rule.Load(cacheDir)
    index := rule.Index(bundles)

    var routeEntries []rule.RouteEntry
    var fallback rule.Target = rule.TargetDirect
    isAllProxy := false

    for i, route := range routes {
        target := rule.ParseVia(route.Via) // k2v5:// → Proxy, "direct" → Direct, "reject" → Reject

        if route.Match.All {
            // match: all → 全局代理
            isAllProxy = (target == rule.TargetProxy)
            fallback = target
            break
        }

        // 展开 match → 规则集名称列表
        names := rule.ExpandMatch(route.Match)

        // 绑定
        var sets []rule.BoundSet
        for _, name := range names {
            if bs, ok := index[name]; ok {
                sets = append(sets, rule.BoundSet{Set: bs, Target: target})
            }
        }

        if len(sets) > 0 {
            routeEntries = append(routeEntries, rule.RouteEntry{Sets: sets, Target: target})
        }

        // 最后一条无 match 的 route 是兜底
        if i == len(routes)-1 && !route.Match.HasCriteria() {
            fallback = target
        }
    }

    return rule.NewEngine(routeEntries, fallback, bundles, isAllProxy)
}
```

### Loader

```go
// rule/loader.go

func Load(cacheDir string) ([]*Bundle, error) {
    // 读取 cache_dir 中所有 .k2b 文件
    // 返回 nil（非 error）如果目录为空或不存在
}

func Index(bundles []*Bundle) map[string]*BundleSet {
    // 从所有 bundle 构建 name→BundleSet 索引
}
```

### 资源所有权

rule.Engine 由 engine.Engine 创建和销毁。Engine.Stop() 中 Close rule.Engine（munmap 所有文件）。

### Lock Ordering

```
rule.Engine.mu (state, RWMutex, standalone — 不嵌套任何其他锁)
```

## k2 集成改动

| 文件 | 改动 |
|------|------|
| `config/config.go` | 删除 `Server`+`Rule` 字段，新增 `Routes []RouteConfig`；`SetDefaults` 从 routes 派生内部 `serverURL` |
| `core/router.go` | 持有 `*rule.Engine`，delegate Match |
| `core/tunnel.go` | `k2rule.Target` → `rule.Target`，SetTmpRule 改为 router 方法 |
| `core/dns/middleware.go` | 注入 matchRule/setTmpRule 函数 |
| `engine/engine.go` | 从 routes 提取 server URL + 构建 rule.Engine |
| `go.mod` | 移除 k2rule |

### Server URL 提取

engine.Start() 中从 routes 提取第一个 k2v5:// URL：

```go
func extractServerURL(routes []config.RouteConfig) string {
    for _, r := range routes {
        if strings.HasPrefix(r.Via, "k2v5://") {
            return r.Via
        }
    }
    return ""
}
```

Phase 1: 只允许一个 server URL。多个 server 报错。
Phase 2: 每条 server route 创建独立的 wire.Dialer。
