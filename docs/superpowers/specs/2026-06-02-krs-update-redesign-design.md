# krs 更新方案重构设计 — Tier 1/2/3

**日期**: 2026-06-02
**状态**: Design (待 plan)
**涉及仓**: `k2-rules`(格式 + CI)、`k2`(rule 包 + engine + krs 消费)、`k2app`(无代码改动,仅部署序)
**前置**: [krs disk-backed memory constitution](2026-06-01-krs-disk-backed-memory-constitution-design.md)、[embedded rules in binary](2026-05-31-embedded-rules-binary-design.md)

---

## 1. 背景与问题

krs 规则更新链路(k2-rules CDN 发布 → k2 `rule/` 下载/校验/原子替换/打开 → 5 平台消费)经一轮架构 review,确认在常见 case 下能正确降级、不崩溃、不毒化缓存,但存在以下**冗余**与**缺陷**:

### 冗余 (R)
- **R1** 双轨 API 全套并行:`LoadNamed`/`OpenNamed`、`RebuildFunc`/`RebuildFuncNamed`、`DownloadAndReload`/`...Named`、`StartUpdater`/`...Named`、`buildRuleEngine`/`buildRuleEngineDisk`。根因:disk-backed 迁移是叠加式的。
- **R2** 非-`Named` 顶层 API(`StartUpdater`/`DownloadAndReload`/`RebuildFunc`)已是死公共面——除测试外零调用方。
- **R3** `bundles.version` 记录 tarball sha256 但从不回读;只读 `"embedded"` 子串和 mtime。sha 是死元数据。
- **R4** 8 路 CDN 全量并发 race + per-source goroutine + `io.ReadAll`,只为抢第一个 200。

### 缺陷 (F)
- **F1**(核心)无"版本身份",只有本地 mtime TTL。多镜像无 newest-wins 仲裁,stale jsdelivr POP 返回旧但合法的包就赢 race 并被冻结 24h。
- **F2** 下载内容零完整性校验:tarball sha256 算了存了从不比对。
- **F3** NE 是唯一无后台 updater 的平台,却把下载(8× `io.ReadAll`)放在 50MB jetsam 进程。
- **F4** 全量 tarball 分发,无增量。
- **F5** 空但结构合法的 `.krs` 绕过所有闸门(CI `validate-krs` 只查 set 名/数,不查规则数;客户端只查文件存在)→ 该区静默全代理。与 all-proxy 灾难同类,深一层。
- **F6** Windows `flock` 是 no-op(潜伏;今天安全仅因 Windows 单进程 + 堆引擎)。

---

## 2. 目标与非目标

### 目标
- 给更新链路一个**可靠的版本身份**,实现跨镜像 newest-wins(解 F1)。
- 下载内容**完整性 + ruleCount 下限**校验,堵空包/降级/损坏(解 F2、F5)。
- **NE 永不下载**,下载收归主 App 进程(解 F3)。
- 削掉死代码与内存浪费(解 R2、R3、R4)。
- Windows flock 真锁(解 F6,低优先)。

### 非目标(YAGNI — 第一性原理裁定)
- **不做签名 / 防篡改**。威胁模型不含"被攻陷的 jsdelivr/GitHub 故意投毒规则";HTTPS + 多镜像一致性足够。我们需要的只是**版本**,不是密码学信任链。→ 砍掉 Ed25519、密钥托管、公钥 go:embed 的全部负担。
- **不做真·分区增量分发(F4 仅留小尾巴)**。Tier 2(a)(NE 不下载)+ Tier 3(流式落盘)已消除内存峰值动机;真增量会重新引入 loose 文件的镜像 staleness 面(参见 `reference_jsdelivr_mirror_staleness`)与 N 个 HTTP 请求,得不偿失。保留单 tarball + manifest 门控。
- **Tier 2(b)(统一单 mmap 引擎 + 删堆 reader)拆出本 spec**,作为紧随其后的独立周期。原因见 §6。

---

## 3. 设计 — 本周期交付 (Tier 1 + Tier 2a + Tier 3)

### 3.1 Tier 1 — 版本驱动的 manifest 门控

**地基已存在**:k2-rules CI 已生成并发布 `manifest.json`,每区已有 `sha256` + `size`,change-detection 已在用它。本设计补齐"客户端消费 + ruleCount + 单调 version"。

#### k2-rules 侧(`main.go::buildManifest` + `tools/validate-krs`)
manifest 升级到 schema v2:
```jsonc
{
  "schemaVersion": 2,
  "version": "2026-06-02T00:31:05Z",   // RFC3339 UTC,严格单调(同日重建可比较)
  "bundles": {
    "cn": { "sha256": "…", "size": 511234, "ruleCount": 28431 },
    "us": { "sha256": "…", "size": 233110, "ruleCount": 12903 }
  }
}
```
- 新增 `ruleCount`(每区 domain + CIDR 规则总数,由 `krs.ReadBundle` 求和)。
- `version` 从日期串 `2006-01-02` 改为 RFC3339 UTC 时间戳。**比较方式**:客户端解析为 `time.Time` 数值比较,解析失败视为最旧;不依赖字符串字典序。
- `validate-krs` 加 publish-time 闸门:每区 `ruleCount > 0`(拦空包 F5);关键区(`cn`)`ruleCount` 不得低于上一份 manifest 的 80%(拦规则数骤降的坏构建)。

#### k2 客户端侧(`rule/manifest.go` 新增 + `ensure.go` 改造)
freshness 从"mtime TTL"升级为"**版本身份**",但**保留快路径零网络**:

```
ensureBundles(force):
  1. 若 !force 且 本地 mtime < shortTTL(=1h):信任本地,return  ← 快路径不变,零网络
  2. 否则(本地超 shortTTL,或 force):
     a. race 镜像拉 manifest.json(小,流式)→ 解析各镜像 version → 取最新 M,记录其来源镜像 srcM
     b. 读本地记录的 manifest version L
     c. 若 M.version <= L 且 本地各区 sha 与 M 吻合 → fresh,刷新 mtime,return(不下整包)
     d. 否则下 tarball:**优先 pin 到 srcM 这个镜像**(H3 化解),失败再 race 其余
     e. 解包后逐区校验:sha == M.bundles[region].sha 且 ruleCount >= M.bundles[region].ruleCount
        - 不符 → 判定"镜像传播中/损坏",保留旧缓存,DIAG 记录,return(不 error-loop,不毒化)
     f. 全部吻合 → atomicSwap → 写 bundles.version = "<M.version> [embedded?]"
  3. manifest 整个拉失败(离线/CDN 全挂)→ 回退现有 mtime+24h TTL 行为(不回归离线/审查场景)
```

- **`bundles.version` 新格式**:`<manifestVersion> [embedded]`。保留 embed marker,使 `wasEmbedSeeded` 仍能识别内置 floor 并在首个 updater tick force-lift;`isVersionFresh` 仍可 mtime 兜底。R3 解决(version 字段现在有意义)。
- newest-wins 的语义边界:是"最新**可达**版本",非"最新**存在**版本"。CN 下若 origin 被墙且所有快响应都是 stale jsdelivr,看到的最新就是 stale 的——这不比今天差,且 embed floor + 旧缓存仍服务。**显式记入已知边界。**

**解决**:F1(newest-wins)、F2(逐区 sha 校验)、F5(ruleCount 下限)、R3(version 有意义)。

### 3.2 Tier 2(a) — NE 永不下载

- `engine.go`:`cfg.NetworkExtension == true` 时,connect-path **只做 `SeedFromEmbedIfEmpty`(必须保留,保证 19 区齐全 → `missingConfiguredRegions` 504 不误触发),跳过联网 `EnsureBundles`**。
- 下载权全归主 App `PrefetchRules`(iOS `AppDelegate.swift:34 AppextPrefetchRules` / Android `MainApplication.kt:27` 已接线),写共享 App-Group/cache 目录;NE 只读。
- 结果:50MB NE 进程内再无 `raceDownload` / `io.ReadAll` 扇出。
- **跨进程 mmap + rename 安全性(载荷性,显式记录)**:主 App 的 `atomicSwap`(rename + orphan `os.Remove`)在 NE 持 `.krs` mmap 时执行;unix(iOS)下 rename/remove 一个被 mmap 的文件,旧 inode 持续有效直至 unmap → NE 续用 connect 时的规则,不 SIGBUS、不读到半写。NE 无 updater(本 tier),整个 session 不 mid-reload,安全。
- **行为变更(接受的取舍)**:从未打开过主 App、仅靠 Connect-On-Demand 触发 NE 的用户,运行在 build-time embed floor 直到首次前台。iOS 几乎必然至少打开一次配置 VPN,属角落场景。

**解决**:F3。

### 3.3 Tier 3 — 廉价加固

- **`fetchBundle` 流式落盘**:`io.Copy(tmpFile, io.LimitReader(body, cap))` 替代 `io.ReadAll`,每路峰值从 ~2.4MB 降到 ~32KB io.Copy 缓冲。这是 F3 内存的**根治**(NE-不下载是消除,流式是即便下载也安全)。
- **`raceDownload` 并发限流**:in-flight ≤ 4,失败即补位(**仍尝试全部 8 源,只是并发上限**),保 CN 穿透韧性的同时不开 8 条并发 TLS。源顺序保证前 4 含可达 CN 镜像。注:流式落地 + NE-不下载后,此项主要是 socket/CPU 收敛,非内存关键。
- **`validate-krs` 最小 ruleCount**:见 §3.1(并入 Tier 1)。
- **Windows 真 flock**:`flock_windows.go` 用 `LockFileEx` 对独立锁文件加锁(进程死亡/句柄关闭自动释放,crash-safe)。**低优先**:Windows 当前单进程,此为防御性正确,近期无实际收益,诚实排最低。
- **删死公共 API**:`StartUpdater`/`DownloadAndReload`/`RebuildFunc` 非-Named(R2)。注:堆 reader `LoadNamed`/`buildRuleEngine` 的删除归 Tier 2(b),本 tier 只删确认零调用方的非-Named updater 三件套。

**解决**:R4、R2、F6、F3(流式)。

---

## 4. 架构师自审 / 风险登记表

> 本节是"再次 review 自己方案"的产物。每条为对**本设计**(非现状)的隐患审查 + 化解。

| # | 隐患 | 严重度 | 化解(已并入设计) |
|---|---|---|---|
| **H1** | Tier 2(b) 全平台统一 mmap 后,Windows `os.Rename`/`os.Remove` 一个被 mmap 持有的文件**会失败**(内核持锁),打挂 updater 的 atomicSwap。unix 全安全,仅 Windows 中招。 | **高** | **将 Tier 2(b) 拆出本周期**(§6)。本周期 Windows 仍用堆引擎,无 live mmap,atomicSwap 安全不变。 |
| **H2** | `DiskBundle.Apps()` 若把全部 app patterns 解进堆,会把 O(rules) 堆压力带回 NE,破宪法。 | 高(限 2b) | 归 Tier 2(b):`Apps()` 懒解码,**仅桌面/Android classify 调用,NE 路由路径永不调**,用 constitution_test 守。 |
| **H3** | manifest 与 tarball 可能来自不同镜像、不同传播态;sha 交叉校验正确拒绝但可 livelock 反复失败。 | 中 | tarball 下载 **pin 到给出胜出 manifest 的同一镜像**;sha 不符判"传播中"→保留旧缓存、下 tick 再试,不 error-loop。(§3.1 d/e) |
| **H4** | 给 fresh 连接路径加 manifest 网络往返 = 连接延迟回归。 | 中 | 版本身份检查**仅在本地 mtime > shortTTL(1h)时触发**;TTL 内信任本地、零网络。Tier 1 是 stale 路径的精化,非 fresh 路径新增税。(§3.1 step 1) |
| **H5** | `bundles.version` 改格式可能破坏 `wasEmbedSeeded` / `isVersionFresh` 兜底。 | 中 | 新格式 `<version> [embedded]` 保留 embed marker;mtime 兜底保留。 |
| **H6** | ruleCount 全局下限会误杀合法的小区。 | 低 | 客户端门 = 配置区 `ruleCount > 0`(拦空);publish 门 = 全区 `>0` + 仅 `cn` 加百分比回归检查。 |
| **H7** | RFC3339 字符串字典序在混入 legacy 日期串时不可靠。 | 低 | 解析为 `time.Time` 数值比较,解析失败视最旧。 |
| **H8** | 并发限流 4 可能拖慢 CN 首次穿透。 | 低 | "≤4 in-flight 且失败补位",仍尝试全部 8 源;源顺序前 4 含可达 CN 镜像。 |
| **H9** | 单区 sha 不符即拒整个 tarball,一个滞后区阻塞全部更新。 | 低 | pin-mirror(H3)后跨镜像 skew 概率极低;fail-closed 不毒化是正确取舍。记入观测。 |

### 满意度结论
- **Tier 1 / Tier 2(a) / Tier 3**(经 H3/H4/H5/H6/H7/H8 打补丁后):内聚、安全、可一次交付。**满意。**
- **Tier 2(b)**:不满意一起做。H1(Windows mmap-vs-rename 回归)+ H2(NE 堆陷阱)是当前"桌面用堆引擎"设计**专门在规避**的;一处错打挂全平台路由。**必须独立 spec/plan/周期**,并显式设计 Windows reload 的 close→swap→reopen 协调(或论证保留 Windows 堆引擎)。

---

## 5. 相位顺序(本周期,每相 TDD,各仓测试绿才进下一相)

1. **Tier 3 止血**(k2 独立):流式落盘 + 并发限流 + 删非-Named 死 API + Windows flock。
2. **Tier 1 k2-rules**:manifest `ruleCount` + 单调 `version` + `validate-krs` 下限。
3. **Tier 1 k2 客户端**:`manifest.go` + 版本身份 freshness + pin-mirror + 完整性门 + `bundles.version` 新格式。
4. **Tier 2(a)**:NE 不下载(engine.go NE 分支跳过联网 EnsureBundles,保 Seed)。

> Tier 2(b) 不在本周期。

---

## 6. Tier 2(b) 为何拆出(给后续 spec 的种子)

统一单 mmap 引擎 + 删堆 reader,需要:
- krs:`DiskBundle.parse()` 加 app section 解码 + `Apps()` 访问器(H2:懒、仅 off-NE)。
- k2:`buildRuleEngineDisk` 填 Apps、`ClassifyInstalled` 走 DiskBundle、全平台 `OpenNamed`、删 `LoadNamed`/`buildRuleEngine`/堆 `NamedBundle` 消费。
- **Windows reload 协调(H1 核心)**:updater 在 mmap 平台必须 download → extract → **engine close 全部 mmap** → atomicSwap → **engine 重开 mmap**;否则 Windows rename 失败。NE(iOS)无 updater 不涉及;macOS/Linux/Android unix rename-under-mmap 安全;**唯 Windows 桌面 daemon 需此协调**。备选:Windows 保留堆引擎(R1 部分解)。

这条路径的 blast radius = 全平台路由,且 H1 需专门设计——配独立 spec。

---

## 7. 测试与置信度

按 `feedback_release_confidence_framework` / `feedback_no_cross_platform_confidence_copy`:**单测绿 ≠ 安全**,以下必须真机/真 CDN smoke,不可只信 mock:
- 跨镜像 newest-wins + pin-mirror + 传播 skew(H3)→ 需真 CDN(含人为 stale 镜像)验证,unit 用 httptest 桩。
- NE 不下载后,Connect-On-Demand 冷触发的规则新鲜度(Tier 2a 行为变更)→ 真机 iOS。
- 跨进程 mmap + 主 App swap 的 NE 续读安全(§3.2)→ 真机 iOS,非 unit。
- Windows flock(H8/F6)→ 真 Windows。

置信度分两个数:**代码机制信心**(读实三仓后)9/10;**业务问题信心**(真能消除对应工单类)封顶 6–7/10,直到上述 smoke 跑过。

## 8. 部署序(载荷性)

1. **k2-rules 先上**:manifest 新字段(schemaVersion/ruleCount/单调 version)+ validate-krs 下限。纯加字段,老 change-detection 与老客户端不受影响。
2. 再发 **k2 客户端**:客户端遇 manifest 缺 `ruleCount` 当"无下限"优雅降级;遇 schema v1 当 version-only。
3. tarball **永不删**(老客户端依赖;新客户端也下载它,只是被 manifest 门控 + 校验)。

> 违反此序(客户端先于 manifest 字段)不致命——客户端优雅降级到现有行为。但仍按此序以获完整收益。

## 9. 已决策点

| 决策 | 结论 | 依据 |
|---|---|---|
| 签名 vs 仅版本 | **仅版本,无签名** | 第一性原理:威胁模型不含投毒,只需版本身份 |
| 分发粒度 | **manifest 门控整包** | Tier 2a/3 已消内存动机,真增量的 staleness 面不值 |
| Tier 2(b) 是否同周期 | **拆出独立周期** | H1 Windows 回归 + H2 NE 堆陷阱,blast radius 全平台路由 |
