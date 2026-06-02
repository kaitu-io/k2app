# krs.BuildSet + rule inline-matcher 收敛 — 设计

> 目标：在 krs 增加一个内存内构造器 `BuildSet`，把 k2 `rule` 包里手写的、与 krs 内部完全重复的域名/IP 匹配实现（`domain.go`/`ip.go`/`inline.go`）删除，让 inline 规则与 bundle 规则共用同一个 `krs.Matcher`。净效果：−~300 行、`RouteEntry` 只剩一种 matcher 类型、消除一整类格式错配 bug。

**状态**：设计定稿，待 review。
**日期**：2026-06-02
**分支策略**（用户已确认）：krs 加 `BuildSet` → tag v0.1.4；k2 改动 stack 在 `feat/krs-disk-consumer` 上，bump go.mod，删 3 文件，fold `Inline → Sets`，一起 ship。

---

## 1. 背景与动机

`rule` 不是 krs 的转发壳——它是把 krs 的叶子 matcher 编排成路由决策的层（route 优先级、App Bypass 元数据匹配、tmp pin、reload 生命周期）。这些 krs 没有，不可删。

但 `rule` 里有**一处真实重复**：`domain.go`（`domainSection` 域名后缀二分）+ `ip.go`（`ipv4Section`/`ipv6Section` IP 区间二分）是手写的、与 krs `NamedSet` 内部**完全等价**的匹配实现。它们的唯一消费者是 `InlineSet`（`inline.go`），唯一生产点是 `engine/engine.go:1181`——把 config 的 `match.domain_suffix` / `match.ip_cidr`（用户内联规则）编译成可匹配结构。

最能说明问题的是 `inline.go` 里 `MatchDomainReversed` 的 `key := rp + "."` 尾点 hack——它存在的唯一原因就是 `rule` 的 domainSection 存 `reverse("."+suffix)`、而 krs 的 reversed parent 是 `reverse(suffix)` 无点，两套格式不一致。收敛掉就消掉这一整类格式错配 bug。

历史上 `domain.go`/`ip.go` 早于 krs 的 `Matcher` 接口存在。krs v0.1.3 已经把 `Matcher`、`NamedSet`、`DiskBundle` 都暴露了，唯独缺一个"从 suffix/CIDR 列表在内存里直接建一个 matcher"的构造器——这正是本设计要补的那一块。

## 2. 范围

**做：**
- krs（k2-rules repo）：新增 `BuildSet` 公开构造器 + `NamedSet.IsEmpty()`；tag v0.1.4。
- k2（rule 包，stack 在 `feat/krs-disk-consumer`）：删 `domain.go`/`ip.go`/`inline.go`（含 `_test`）；删 `RouteEntry.Inline` 字段 + `InlineSet`/`BuildInlineSet`；assembly 改走 `krs.BuildSet`。

**不做：**
- 不动 `rule` 的编排/生命周期文件（engine 的路由逻辑、download/ensure/updater/embed/classify/target/loadnamed/opennamed）。
- 不改 krs 对无效 CIDR 的静默丢弃行为（既有行为，见 §6.3）。
- 不碰 OOM/jetsam 路径——本变更与崩溃修复无关，是纯技术债清理。
- 不动分支上未提交的 instrumentation 文件（`appext.go`/`config/log.go`/`crashoutput_test.go`），保持 unstaged。

## 3. 组件设计

### 3.1 krs：`BuildSet` 构造器

```go
// BuildSet compiles s's write-input fields (DomainSuffixes, ExcludeDomains,
// CIDRs) into in-memory match state and returns it ready to use as a Matcher.
// Normalization is identical to WriteBundle→ReadBundle, so a directly-built
// set matches exactly as a file-loaded one.
//
// Intended for setup-time construction (e.g. compiling a few inline config
// rules), NOT the per-match hot path: it serializes + parses a one-set bundle.
func BuildSet(s NamedSet) (*NamedSet, error) {
	var buf bytes.Buffer
	if err := WriteBundle(&buf, &Bundle{Sets: []NamedSet{s}}); err != nil {
		return nil, err
	}
	b, err := ReadBundle(buf.Bytes())
	if err != nil {
		return nil, err
	}
	return &b.Sets[0], nil
}

// IsEmpty reports whether the set has no positive match rules (domain
// suffixes or IP ranges). An exclude-only set is considered empty because
// excludes alone match nothing.
func (s *NamedSet) IsEmpty() bool {
	return len(s.domainSection.reversed) == 0 &&
		len(s.ipv4.starts) == 0 &&
		len(s.ipv6.starts) == 0
}
```

**为什么用 round-trip 实现，而不是抽取共享 helper：**

round-trip（`WriteBundle`→`ReadBundle`）**天然正确**——round-trip 就是"像真 bundle 一样归一化"的定义，零风险出现第二条与 writer 漂移的归一化路径。代价是一次 serialize+parse（仅 setup 时、非热点）。被否决的替代方案（抽取 `normalizeSuffixesReversed`/`buildIPSections` 共享 helper）更快，但引入一条"必须永远与 writer 字节一致"的代码路径，且仍需一个 parity 守卫测试——对 setup-time 用途不值得。

返回 `*NamedSet`（而非裸 `Matcher`）：Matcher 方法是指针接收者；且给消费者 `IsEmpty()` 以判断该 inline route 是否真的贡献 host criteria。`*NamedSet` 满足 `Matcher`，可直接 append 进 `[]krs.Matcher`。

**已实证**（2026-06-02 scratch 测试，5/5 绿）：domain/IDN(`例え.jp`)/子域/v4/v6/exclude/empty/all-invalid→empty/parity 全部通过。结构事实已确认：`NamedSet` 非导出字段为 `domainSection`/`excludeSection`/`ipv4`/`ipv6`；`collectSections` 在 `len(Sets)>0` 时恒发 SetTable 且跳过空 payload，所以单集合、仅域名、Apps=nil 的 bundle 干净 round-trip。

### 3.2 k2：fold `Inline → Sets`

删除 `RouteEntry.Inline *InlineSet` 字段。assembly（`engine/engine.go:1181` 附近）：

```go
ns, err := krs.BuildSet(krs.NamedSet{
	DomainSuffixes: route.Match.DomainSuffix,
	CIDRs:          route.Match.IPCIDR,
})
if err != nil {
	slog.Warn("rule: skip inline set (build failed)", "err", err)
} else if !ns.IsEmpty() { // 守卫：全无效输入 ⇒ 不贡献 host criteria
	entry.Sets = append(entry.Sets, ns)
}
```

`matchRouteDomain`/`matchRouteIP`/`hasHostCriteria` 去掉各自的 `r.Inline` 分支——`RouteEntry` 此后只有一个 matcher 列表 `Sets []krs.Matcher`。

## 4. 数据流

```
config route.Match{DomainSuffix, IPCIDR}
  → krs.BuildSet(NamedSet{...})            // setup 时一次 serialize+parse
  → *krs.NamedSet (satisfies Matcher)
  → 若 !IsEmpty: append 到 RouteEntry.Sets
  → 匹配时: matchRouteDomain(parents) 遍历 Sets[].MatchDomainReversed(parents)
            matchRouteIP(addr)        遍历 Sets[].MatchIP(addr)
```

inline matcher 与 bundle matcher 在 `Sets` 里同质，走同一条匹配热路径，归一化（`krs.ReversedParents`）由调用方每次查询算一次（constitution rule 5），不受影响。

## 5. 测试策略

### 5.1 krs（TDD）
- `BuildSet`：匹配域名、IDN、子域、v4、v6；respect excludes；drop 无效域名（同 writer）；`IsEmpty` 对 empty / all-invalid / **exclude-only** 返回 true；parity vs `Index(ReadBundle(WriteBundle(...)))`。
- krs 全包 `go test ./...` + `-race` 绿；CI 结构门（publish-time `.krs` gate）不受影响（本变更不产出 `.krs` 制品）。

### 5.2 k2（行为保持铁证）
- **重写** `rule/engine_test.go` 的 `TestEngineMatchConn_InlineDomain`（`custom-blocked.com` → Target 2）与 `TestEngineMatchConn_InlineIP`（`10.0.0.0/8` → Target 0）到新路径（`krs.BuildSet` append 进 `Sets`），**断言不变**，全绿——这是行为字节级保持的核心守卫。
- **新增**守卫：一个 inline 输入全无效（如 `domain_suffix:["_bad_"]`）的 route，`hasHostCriteria()` 为 false，不被误当成 meta-only 阻断匹配。
- `rule` + `engine` 全测试 `-short` + `-race` 绿（wire/engine race 需 ≥300s）；`golden_test.go` 绿。

## 6. 错误处理与边界

### 6.1 BuildSet error
透传 `WriteBundle`/`ReadBundle` 的 error（合法 in-memory 输入实际上不会 error）。rule 侧 `err!=nil → WARN + 跳过该 inline matcher`，路由降级为"该 route 无 inline host criteria"，与坏 bundle 的降级一致。

### 6.2 空 / 全无效输入
`IsEmpty()` 为 true 时 rule **不** append——避免空 matcher 让 `hasHostCriteria()` 误判为 true（否则会污染 meta-only route 的语义）。这是 §5.2 新增守卫测试要钉死的点。

### 6.3 无效 CIDR 静默丢弃（既有行为，不改）
krs `parseCIDRsByFamily` 对无法解析的 CIDR 静默 `continue`（无 warn）；无效域名则 drop+warn。rule 老的 `BuildInlineSet` 同样静默丢无效 CIDR（`netip.ParsePrefix` err → continue）。**行为保持**，本次不引入新 warn（那会改 krs writer 行为，超出范围）。仅在此标注为已知既有行为。

## 7. 版本 / 部署

- dev：k2 `go.mod` 加临时 `replace github.com/kaitu-io/k2-rules => ../k2-rules`，对本地 krs build/test。
- **finalization 清单**（仅在用户批准 merge 时执行）：移除 `replace` → push k2-rules → tag `v0.1.4` → k2 `go get github.com/kaitu-io/k2-rules@v0.1.4` → 确认 go.mod/go.sum 干净。
- krs v0.1.4 是纯代码库变更，**无 CDN 制品**，无 `.krs` 部署序问题。

## 8. 发布信心与前置条件

**本变更（krs.BuildSet + k2 fold）desk 侧可达 10/10**：行为字节级保持、不引入新运行时行为、新库函数已实证、所有行为保持守卫全绿。对一个行为保持的重构，单元/race 全绿即充分发布证据。

**整条发布线的硬前置（无法由本变更消除）**：本改动 stack 在 `feat/krs-disk-consumer` 上，后者的真机验证（disk-reader Plan 2 Task 11）尚未完成。**本 refactor 的"全绿"不得被解读为整条线可发布**——disk-reader 的真机验证是地基，必须先过。这是唯一的剩余风险来源，且属于 disk-reader 而非本变更。

## 9. 文件清单

**krs（k2-rules repo）**
- 新：`krs/buildset.go` — `BuildSet` + `NamedSet.IsEmpty()`
- 新：`krs/buildset_test.go`
- tag：`v0.1.4`

**k2（rule 包，feat/krs-disk-consumer）**
- 删：`rule/domain.go`、`rule/ip.go`、`rule/inline.go`、`rule/domain_test.go`、`rule/ip_test.go`、`rule/inline_test.go`
- 改：`rule/engine.go`（删 `RouteEntry.Inline`、`InlineSet` 引用、`matchRouteDomain/IP/hasHostCriteria` 去 Inline 分支）
- 改：`rule/engine_test.go`（重写 `TestEngineMatchConn_InlineDomain/InlineIP` + 新增 all-invalid 守卫）
- 改：`engine/engine.go`（assembly 改 `krs.BuildSet`）
- 改：`go.mod`（v0.1.3 → v0.1.4，dev 期 replace）
- 查：`golden_test.go` 等是否引用 `BuildInlineSet`/`InlineSet`，同步更新
```
