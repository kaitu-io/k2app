# k2 Rule Module Design Spec

> 替代 k2rule，实现按服务分类的多规则集路由引擎

## 问题

k2rule v0.6.4 的单文件模型依赖 DNS pipeline 的 `SetTmpRule` 来标记 IP。当 app（WhatsApp、Telegram 等）使用硬编码 IP 或私有 DNS 协议绕过 k2 的 DNS 时，`SetTmpRule` 不会触发，IP 被错误地路由为 DIRECT。

业界所有主流工具（Clash/Mihomo、sing-box、Surge、v2ray）通过维护**按服务分类的 IP-CIDR 列表**解决此问题。k2 需要同样的能力。

## 设计

### 两阶段匹配模型

```
阶段 1（DNS 时）: Match(domain) → action
                  → SetTmpRule(learned IPs, action)

阶段 2（连接时）: Match(destIP)
                  → TmpRule（DNS 动态学习）     最高优先级
                  → Service IP-CIDR 规则集      静态规则
                  → GeoIP 国家级规则             兜底
                  → default action              最低优先级
```

SetTmpRule 覆盖 CDN 动态 IP（不在任何已知 IP 段中、但刚被 DNS 解析出来的 IP）。静态 IP-CIDR 覆盖硬编码 IP（WhatsApp 走 Meta IP 段、Telegram 走官方 CIDR）。两者互补。

### 规则集 = 纯匹配数据（headless）

规则集文件只包含"这些域名和 IP 属于某个服务"，不包含 action。action 由用户配置绑定：

```
.k2b 文件: { name: "google", domains: [...], ips: [...] }   ← 纯数据
config:    { name: "google", action: proxy }                  ← 用户语义
```

同一个 google.k2b 在中国用户手里是 `action: proxy`（翻出去），在海外用户手里可以是 `action: direct`（不需要代理）。

### 规则集按公司/基础设施组织

不按具体服务（WhatsApp、Instagram）拆分，而按拥有独立 IP 段的公司/基础设施拆分：

| 规则集 | 域名覆盖 | IP-CIDR 覆盖 |
|--------|---------|-------------|
| google | google.com, youtube.com, googleapis.com, gstatic.com, ... | Google 官方 IP 段 |
| facebook | facebook.com, whatsapp.com, instagram.com, messenger.com, ... | Meta ASN IP 段 |
| telegram | telegram.org, t.me, ... | Telegram 官方 CIDR |
| twitter | twitter.com, x.com, ... | Twitter/X ASN IP 段 |
| netflix | netflix.com, nflxvideo.net, ... | Netflix ASN IP 段 |
| cloudflare | — | Cloudflare 官方 IP 段 |
| discord | discord.com, discordapp.com, ... | 无独立 IP（用 Cloudflare CDN） |
| spotify | spotify.com, ... | 无独立 IP（用 Google Cloud/Fastly） |
| tiktok | tiktok.com, ... | 无独立 IP |
| openai | openai.com, chatgpt.com, ... | 无独立 IP（用 Cloudflare/Azure） |
| github | github.com, githubusercontent.com, ... | 无独立 IP |
| geoip-cn | — | 中国 IP 段（APNIC CN 分配） |
| cn-sites | 中国常用网站域名 | — |

无独立 IP 段的服务只有域名规则，依赖 DNS 阶段 + SetTmpRule 覆盖 — 与现有行为一致。

## 数据源与许可证

| 数据源 | 许可证 | 用途 | 合规方式 |
|--------|--------|------|---------|
| v2fly/domain-list-community | MIT | 域名列表 | 运行时下载，附署名 |
| Loyalsoldier/geoip | CC-BY-SA-4.0 | 按服务 IP-CIDR | 运行时下载，附署名，不修改数据 |
| Google gstatic.com/ipranges/goog.json | 公开数据 | Google IP 段 | 无限制 |
| Telegram core.telegram.org/resources/cidr.txt | 公开数据 | Telegram IP 段 | 无限制 |
| Cloudflare cloudflare.com/ips-v4 | 公开数据 | Cloudflare IP 段 | 无限制 |
| APNIC 分配数据 | 公开数据 | 中国 IP 段 | 无限制 |
| go4.org/netipx | BSD-3-Clause | IP 集合匹配（编译器用） | 附 license notice |

**不可用的数据源**（GPL 感染风险）：
- sing/common/domain (GPL-3.0) — 不导入，自建域名匹配
- SagerNet/sing-geoip, sing-geosite (GPL-3.0) — 不使用编译产物
- metacubex/meta-rules-dat (GPL-3.0) — 不使用编译产物

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

相比 k2rule v3 的 O(n) 线性扫描（逐条 CIDR mask 比较），改进为 O(log n)。Google 5000+ CIDR 合并为 ~2000 Range 后，查找从 5000 次比较降为 ~11 次。

### IPv6 区布局

```
Ranges[Count]: (from[16] + to[16])[]  每条 32 bytes，同 IPv4 逻辑
```

### 体积预估

| 内容 | 磁盘（mmap 格式） | 下载（gzip 传输） | 内存峰值（活跃页） |
|------|-----------------|-----------------|------------------|
| overseas 预设 (15 服务) | ~200KB | ~60KB | ~16KB |
| cn-direct | ~130KB | ~35KB | ~12KB |
| 典型用户总计 | ~330KB | ~95KB | ~20KB |

对比 k2rule: .k2r ~2MB + .mmdb ~5MB = 7MB 磁盘。缩小 20 倍。

### 分发方式

```
下载: HTTP GET overseas.k2b.gz (~60KB, Content-Encoding: gzip)
  → 解压写入 cache_dir/overseas.k2b (~200KB)
  → mmap(cache_dir/overseas.k2b)
  → 就绪，零解析
```

## rule 包设计

### 包结构

```
rule/
├── engine.go       匹配引擎 (Engine, Match, SetTmpRule)          ~300 行
├── format.go       k2b reader/writer                            ~250 行
├── domain.go       域名二分查找 (复用 k2rule v3 算法)             ~100 行
├── ip.go           IP Range 二分查找                             ~80 行
├── loader.go       manifest 下载 + 缓存 + mmap                  ~200 行
├── json.go         JSON 源格式解析 (调试/自定义规则)              ~80 行
└── engine_test.go  测试                                         ~200 行
```

预估总计: ~1200 行（含测试）

### 核心类型

```go
package rule

// Target 路由决策
type Target uint8

const (
    TargetDirect Target = iota
    TargetProxy
    TargetReject
)

// RuleSet 一个服务的匹配数据（从 mmap 引用，不拥有内存）
type RuleSet struct {
    Name    string
    domains domainSection  // mmap 切片引用
    ipv4    ipv4Section    // mmap 切片引用
    ipv6    ipv6Section    // mmap 切片引用
}

// BoundSet 绑定了 action 的规则集（配置层）
type BoundSet struct {
    RuleSet *RuleSet
    Target  Target
}

// Engine 多规则集匹配引擎
type Engine struct {
    mu       deadlock.RWMutex
    tmp      map[netip.Addr]Target   // DNS 动态学习（最高优先级）
    sets     []BoundSet              // 按配置顺序排列
    geoIP    []BoundSet              // 国家级 GeoIP（最低优先级）
    fallback Target                  // 默认 action
    bundles  []*mmapBundle           // mmap 文件句柄（Engine 关闭时 munmap）
}
```

### Match 算法

```go
func (e *Engine) Match(host string) Target {
    e.mu.RLock()
    defer e.mu.RUnlock()  // RLock 临界区只做内存读，≤10 行，defer OK

    addr, isIP := parseAddr(host)

    if isIP {
        // 1. TmpRule（DNS 动态学习）
        if t, ok := e.tmp[addr]; ok {
            return t
        }
        // 2. Service IP-CIDR（first-match-wins）
        for i := range e.sets {
            if e.sets[i].RuleSet.MatchIP(addr) {
                return e.sets[i].Target
            }
        }
        // 3. GeoIP
        for i := range e.geoIP {
            if e.geoIP[i].RuleSet.MatchIP(addr) {
                return e.geoIP[i].Target
            }
        }
    } else {
        // 域名匹配
        domain := strings.ToLower(host)
        for i := range e.sets {
            if e.sets[i].RuleSet.MatchDomain(domain) {
                return e.sets[i].Target
            }
        }
    }

    return e.fallback
}
```

RWMutex 用 RLock（读操作，高频并发）。SetTmpRule 用 WLock。临界区只做 map 查找 + mmap 二分查找，符合 CLAUDE.md 并发规则。

### SetTmpRule

```go
func (e *Engine) SetTmpRule(ip string, target Target) {
    addr, err := netip.ParseAddr(ip)
    if err != nil {
        return
    }
    e.mu.Lock()
    e.tmp[addr] = target
    e.mu.Unlock()
}
```

## 客户端配置

```yaml
rule:
  # 简单模式（一行搞定 90% 场景）
  preset: overseas        # 展开为 google=proxy, facebook=proxy, telegram=proxy, ...

  # 中国直连
  cn_direct: true         # 展开为 geoip-cn=direct, cn-sites=direct

  # 默认 action（所有规则未命中时）
  default: direct         # direct | proxy

  # 高级：显式规则集（合并到 preset 展开结果中，同名覆盖）
  sets:
    - name: openai
      action: reject      # 覆盖 preset 中 openai 的默认 proxy
    - name: my-company
      url: "https://example.com/rules/company.k2b"
      action: direct

  cache_dir: ""           # 由平台 shell 设置
```

### 预设展开规则

preset 展开为一组 `{name, action}` 绑定。用户的 `sets` 合并到展开结果中，**同名覆盖**：

```
preset: overseas 展开:
  google=proxy, facebook=proxy, telegram=proxy, twitter=proxy,
  netflix=proxy, discord=proxy, spotify=proxy, tiktok=proxy,
  openai=proxy, github=proxy

用户 sets: [{ name: openai, action: reject }]

最终: google=proxy, facebook=proxy, ..., openai=reject, ..., github=proxy
```

### 翻回来场景

```yaml
rule:
  preset: cn-access       # 展开为 cn-sites=proxy, bilibili=proxy, geoip-cn=proxy
  default: direct
```

## 数据管道（独立仓库 kaitu-io/k2-rules）

GitHub Actions 每日构建:

```
1. 拉取上游源
   ├── v2fly/domain-list-community (MIT) → data/{service} 文件
   ├── Loyalsoldier/geoip (CC-BY-SA-4.0) → text/{service}.txt
   └── 官方 IP 发布 (Telegram cidr.txt, Google goog.json, etc.)

2. 编译
   ├── 解析上游格式 → 统一内部表示
   ├── IP: netipx.IPSetBuilder 合并相邻/重叠 CIDR → 排序 Range 对
   ├── Domain: 反转 + 排序 + dot 前缀
   ├── 打包为 .k2b bundle (overseas.k2b, cn-direct.k2b, etc.)
   └── 生成 manifest.json (版本, sha256, 大小)

3. 发布
   ├── GitHub Releases
   ├── CDN 分发 (jsdelivr / 自有 CDN)
   └── ATTRIBUTION.md (满足 CC-BY-SA 署名)
```

### manifest.json

```json
{
  "version": "2026-04-02",
  "base_url": "https://cdn.example.com/rules/",
  "bundles": {
    "overseas": {
      "version": "2026-04-02",
      "sha256": "abc123...",
      "size": 198400,
      "sets": ["google", "facebook", "telegram", "twitter", "netflix",
               "discord", "spotify", "tiktok", "openai", "github",
               "cloudflare", "fastly"]
    },
    "cn-direct": {
      "version": "2026-04-02",
      "sha256": "def456...",
      "size": 134200,
      "sets": ["geoip-cn", "cn-sites"]
    },
    "cn-access": {
      "version": "2026-04-02",
      "sha256": "ghi789...",
      "size": 89600,
      "sets": ["cn-sites", "bilibili", "geoip-cn"]
    }
  },
  "presets": {
    "overseas": {
      "bundles": ["overseas"],
      "defaults": { "*": "proxy" }
    },
    "cn-access": {
      "bundles": ["cn-access"],
      "defaults": { "*": "proxy" }
    }
  }
}
```

### JSON 源格式（调试 + 自定义规则）

```json
{
  "name": "google",
  "version": "2026-04-02",
  "domains": {
    "suffix": ["google.com", "googleapis.com", "youtube.com", "gstatic.com"]
  },
  "cidrs": ["8.8.4.0/24", "8.8.8.0/24", "8.34.208.0/20", "2001:4860::/32"]
}
```

客户端可直接加载 JSON（启动时编译到内存），或用 `k2 rule compile foo.json` 预编译为 .k2b。

## k2 集成改动

### 接口替换（类型级别）

| 文件 | 改动 |
|------|------|
| `engine/engine.go` | `k2rule.Init(cfg)` → `rule.NewEngine(cfg)` + `rule.Load()`; Engine 持有 `*rule.Engine` |
| `core/router.go` | `k2rule.Match()` → `e.ruleEngine.Match()`; Router 持有 `*rule.Engine` 引用 |
| `core/tunnel.go` | `k2rule.Target` → `rule.Target`; `k2rule.SetTmpRule` → `e.SetTmpRule` (3 处) |
| `core/dns/middleware.go` | 同上 (2 处 SetTmpRule + Target 类型) |
| `config/config.go` | `RuleConfig` 重写: Preset, Sets, Default, CnDirect, CacheDir |
| `go.mod` | 移除 `github.com/kaitu-io/k2rule`; 无新增外部依赖（netipx 仅编译器用） |

### Router 改造

```go
// core/router.go
type Router struct {
    engine *rule.Engine
}

func NewRouter(engine *rule.Engine) *Router {
    return &Router{engine: engine}
}

func (r *Router) Route(info packet.Info) rule.Target {
    return r.engine.Match(info.DstIP)
}

func (r *Router) RouteAddr(host string) rule.Target {
    return r.engine.Match(host)
}
```

### Engine 初始化

```go
// engine/engine.go Start() 中
ruleEngine, err := rule.NewEngine(rule.Config{
    Preset:   client.Rule.Preset,
    Sets:     client.Rule.Sets,
    CnDirect: client.Rule.CnDirect,
    Default:  client.Rule.Default,
    CacheDir: client.Rule.CacheDir,
})
if err != nil {
    return e.fail(err)
}
e.ruleEngine = ruleEngine
```

### 资源所有权

rule.Engine 由 engine.Engine 创建和销毁。Engine.Stop() 中 Close rule.Engine（munmap 所有文件）。符合 CLAUDE.md 资源所有权规则。

### Lock Ordering Graph 更新

```
rule.Engine.mu (state, RWMutex, standalone — 不嵌套任何其他锁)
```

rule.Engine.mu 是独立的 RWMutex。Match 从 tunnel goroutine 调用（RLock），SetTmpRule 从 dns/middleware goroutine 调用（WLock），两个调用路径均不持有 engine.mu 或其他锁。无嵌套风险。
