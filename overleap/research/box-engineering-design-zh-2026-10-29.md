# Overleap 盒子：工程组织与运行时设计

- 日期：2026-10-29
- 状态：**设计稿，待 David 批准**。批准前不建目录、不写代码。实现、上线、花钱都要 David 本人确认。
- 前序：`2026-10-16-overleap-home-exit-network-design.md`（v2，对等互换）。本稿只管“盒子怎么做、代码放哪、怎么升级”。产品和商业的决定以 `kaitu-io/overleap/docs/decisions.md` #15–#20 为准，v2 里和它们冲突的地方（人人买设备、积分只换时长、出口数 × 20 ÷ k）作废。
- **计费不在本稿范围**。David 10-29 说“对于计费，需要重新设计”，计费单独出设计稿。本稿只定盒子要给计费提供的接口（第 6 节）。

## 1. 为什么做、做成什么样

Overleap 是一张家庭出口网络：主机把一台小盒子接在自家宽带上，别人的流量从这台盒子出去，主机按真实流量拿钱；用户装 App 就能用，不必先买盒子（#15）。这次立项的重点就是盒子：一台有线、不带 Wi-Fi、不接管家里网关的小设备（#16）。

David 的要求，按原话归纳：
- 和开途是完全不同的产品，对外看不出关系。
- 盒子用 Go，延续 k2 的能力（#18、#19）。
- 用户和盒子之间最终要能 P2P 直连；协议参考 k2v5，拥塞控制借鉴 k2cc。
- 盒子是硬件，系统升级要做得“足够足够的好，能够自动升级”。
- “我们没有试错成本可以承担，必须要预判”。

### 目标
1. 一台盒子在 100 Mbps 上行档稳定转发 k2v5 加密流量，同时服务约 50 人。硬件可以选更高一档（#19）。
2. 盒子装好后不需要主机再操作：自动挂接、自动升级、升级失败自动回退。
3. 盒子程序和开途的运行时共用 k2 的代码，但编译出来的二进制、镜像、日志、服务名里看不到 `kaitu` 或 `开途`。
4. 对外承诺“不记录访问的网站”，在盒子代码里有实现和测试。

### 不做
- 不 fork Tailscale、NetBird、EasyTier 这类开源组网项目（调研结论见第 3 节）。
- 不开源（#5 维持闭源）。
- 不做 OpenWrt 包，不做路由器形态，不做 Wi-Fi。
- 不在本稿里定计费规则、单价和提现。

### 失败的样子（技术上能跑，但算失败）
- 升级把盒子变砖，需要主机拔电源或寄回来。
- 盒子日志或上报里出现了用户访问的域名或 IP。
- strings 扫出 `kaitu`、`开途` 或 `kaitu-io`。
- 主机改了盒子程序就能多领钱。

## 2. 约束

| 约束 | 来源 | 影响 |
|---|---|---|
| Go 在 32 位 ARM 和 riscv64 上没有 ChaCha20、AES-GCM、Poly1305 的汇编 | 调研，见 golang/x/crypto 和标准库源码 | 盒子最低是带 ARMv8 加密扩展的 arm64，加密套件优先 AES-128-GCM |
| Go 的 pclntab 会留下包路径，`-s -w` 去不掉 | 品牌守卫 | 构建时要把 k2 的模块名换成中性的名字（第 5 节） |
| k2 是 Overleap 和开途共用的运行时 | #18、#20 | 对两边都有用的改动进 k2；只属于盒子的逻辑放 `overleap/box` |
| tailscaled/wireguard-go 一类 Go 程序常驻内存约 90 MB，缓冲池可能涨到 OOM | 调研 | 内存至少 512 MB，盒子程序要设 GOMEMLIMIT 并给连接数和缓冲设上限 |
| SD 卡写坏率高，写入中断电会损坏 | 调研 | 系统放 eMMC，根文件系统只读，日志写在内存里 |
| 主机能读能改盒子程序 | #5 闭源也挡不住逆向 | 计费不信任盒子自报（第 6 节） |

## 3. 考虑过的做法

### 3.1 用什么做底层

| 做法 | 结论 |
|---|---|
| fork Tailscale + Headscale（BSD-3） | 否。它是整机 VPN（L3），按“用户 × 网站组”给不同的盒子分流要大改；WireGuard 的特征换不掉；每周发版，跟进成本高 |
| NetBird / Firezone / EasyTier | 否。没有决定性优势；EasyTier 是去中心化模型，和 Center 统一分配冲突 |
| **在 k2 上做（选中）** | k2 是 L4 代理，按目标域名选出口本来就能做；k2v5 有伪装；计量和不记日志都在自己代码里 |

打洞的做法可以参考 Tailscale magicsock（BSD-3）。

### 3.2 用什么语言

David 一开始提 Rust，后来定 Go（#19）。理由：k2v5、k2cc、k2p 都是 Go，直接复用；Rust 版 k2（k2.rust）3 月 5 日后没有更新，缺 k2cc 的自适应步长和 k2p。Go 带来的限制是必须用 arm64，这已经算进硬件选择里。

### 3.3 代码放哪

| 做法 | 结论 |
|---|---|
| A. 盒子程序写进 k2 仓库，像 k2r 一样加一个 `cmd/` | 否。Overleap 的产品代码会放在开途的仓库里 |
| **B. `k2app/overleap/` 下建独立的 Go 模块，通过 replace 引用 k2（选中，#20）** | 盒子的逻辑归 Overleap；通用的改进回到 k2，开途也受益 |
| C. 另开仓库 fork k2 | 否（#18）。fork 后和 k2cc 等后续改进越走越远 |

### 3.4 k2 是子模块还是并进 k2app

David 提出把 k2 从子模块并进 k2app，并让我来做。这是一个独立的迁移，和本稿没有先后依赖，计划见附录 A。本稿的目录和构建都按“并进以后，k2 在 `k2app/k2/`”来写；如果迁移推迟，replace 路径照样是 `../../k2`，不受影响。

## 4. 目录和职责

```
k2app/
  k2/                         共用运行时（并入后仍是 github.com/kaitu-io/k2）
    wire/ k2cc/ k2p/ server/   盒子要用、开途也会用的改进放这里
  overleap/
    CLAUDE.md                 本目录的规则：品牌守卫、隐私承诺、升级不许变砖
    box/                      Go 模块 overleap.example/box（模块名待定，第 9 节）
      go.mod                  replace github.com/kaitu-io/k2 => ../../k2
      cmd/olbox/              盒子主程序
      attach/                 连入口：挂接会话、重连、心跳
      exit/                   出口：出站拨号、屏蔽规则、限速、内置 DoH
      receipt/                计费收据的盒子端（只转交，不签发，第 6 节）
      health/                 健康检查：给升级判断“新版本是否正常”
      updater/                程序级 A/B 升级
    os/                       系统镜像：板级支持、RAUC 配置、U-Boot 启动计数
    ota/                      发版清单格式、签名脚本、发布到静态服务器
    build/
      rebrand.sh              复制 k2 到临时目录、改模块名、编译
      guard.sh                strings 检查：kaitu / 开途 / kaitu-io 一律失败
  api/                        Center；用户、盒子、分配都按 brand=overleap 隔开（api/brand.go 已有 BrandOverleap）
  sites/overleap/             官网，不动
```

### 4.1 改动放 k2 还是放 overleap/box

放 k2 的条件是**开途也会用到，或者属于协议本身**：
- k2p 主机端的生产入口、信令鉴权（v2 第 1 节：`metadata_auth.go` 的 validator=nil）
- k2s 的处理器不记录目的地（前置修复，见 overleap/docs/open-questions.md）
- 出口挂接（v2 第 3.1 节的 `mode=exit-attach`）和挂接会话包成 `net.Listener` 的适配器
- 屏蔽内网、端口、BT 的出站检查，如果开途也想要

只属于盒子的放 `overleap/box`：主程序、升级、健康检查、收据转交、品牌化的配置和默认 Center 地址。

改 k2 的部分照 k2 现有的规矩走：先写测试，由 labtest 跑。

## 5. 换品牌构建

1. `build/rebrand.sh`：把 `k2/` 复制到构建用的临时目录，用 `go mod edit -module` 和批量改 import 把 `github.com/kaitu-io/k2` 换成中性的模块名，`overleap/box` 的 replace 同时指过去。
2. `CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -trimpath -ldflags "-s -w -buildid="`。
3. `build/guard.sh`：对二进制和整个系统镜像跑 `strings`，扫到 `kaitu`、`开途`、`kaitu-io` 就失败。第三方依赖（比如 `third_party/kuic`）的模块名也要查。
4. 这一步放进 CI，每次构建都跑。guard 本身要有一个测试：故意放一个带 kaitu 的二进制，确认它会失败。

## 6. 盒子和计费的接口

计费规则另外设计。本稿只定一条原则：**盒子不签发计费凭据**。

- 用户的 App 按入站字节对累计量签收据（设备密钥加单调序号），交给盒子；盒子只负责转交给 Center。
- 走中继的流量，Center 再拿中继的计数核对一遍。
- 盒子自报的字节数只用来做监控和容量调度，不用来付钱。

`receipt/` 包只实现“收、存、转交”。收据格式、签名算法和兑付规则在计费设计稿里定。

## 7. 系统和升级

分两层，各自独立回退：

| 层 | 内容 | 频率 | 机制 |
|---|---|---|---|
| 系统 | 内核、只读根文件系统、RAUC | 很少 | RAUC A/B 两个系统槽 + U-Boot 启动计数；包必须签名；新系统启动后 `olbox` 健康检查通过才 `mark-good`，否则重启几次后自动回到旧槽 |
| 程序 | `olbox` 二进制 | 经常 | 数据分区里放两个版本 + 一个指向当前版本的 symlink；ed25519 签名；新版本启动后 N 分钟内连不上 Center 或健康检查失败，就切回旧版本并上报 |

其他：
- 升级包放在普通的 HTTPS 静态服务器上，不用 Mender（增量更新要付费）。RAUC 1.7 起可以直接从 HTTPS 流式安装。
- 分批发布：先 1%，再 10%，再全部。哪台盒子在哪一批，由 Center 下发的清单决定。
- 存储用 eMMC，开 pSLC 模式；日志写在内存里，只上报汇总，不落盘。
- 签名私钥不放在 CI 里能直接读到的地方，存放方式由 David 定（第 9 节）。

## 8. 硬件

- 底线：带 ARMv8 加密扩展的 arm64、千兆网口、内存 ≥ 512 MB、eMMC。
- 首选 RK3528A（4×A53 2.0 GHz，芯片约 $7）。第二供应商：全志 H618。更高一档用 RK3576（A72 级）。
- 量产物料估算 $30–45，变数主要是内存价格。
- **先实测再定**：买 RK3528A 两块、RK3566 一块、RK3576 一块，用 50 个并发客户端测 k2v5 的吞吐和常驻内存，大约 $100。花钱要 David 批准。
- 每个吞吐数字都是从内核 WireGuard 的数据推算的，ARM 上 quic-go 的实测一条都没有，所以必须实测。

## 9. 待 David 定

1. `overleap/box` 的 Go 模块名，以及换品牌后 k2 的中性模块名。
2. 升级签名私钥放在哪里，谁能发版。
3. 实测用的板子，约 $100。

## 10. 留给实现者

- 包内的接口划分、配置格式、日志字段（不能有目的地）。
- RAUC 和 U-Boot 的具体配置，要按选定的板子来。
- 健康检查的具体指标和超时时间。

## 附录 A：k2 并入 k2app 的迁移计划

另走一个分支，不和盒子的代码混在一起。

**前提**：
- k2app-a1 的 `fix/smux-half-close` 合进 k2 master，并更新 k2app 的 gitlink。
- `docs/resumable-tcp-streams` 迁移后在 k2app 里重开。
- `canary/k2cc-lossattr-on-fleet` 还有 1 个提交没合，等 David 决定。旧仓库保留只读，这个提交不会丢。

**步骤**：
1. 在独立 worktree 里删除 k2 子模块，用 `git subtree add --prefix=k2` 并入 k2 master，保留历史。
2. 嵌套的 `third_party/kuic` 仍然是子模块：在根目录的 `.gitmodules` 里加 `k2/third_party/kuic`，CI 继续用 `KUIC_DEPLOY_KEY` 读它。
3. 改 7 个 workflow（ci、release-desktop、release-k2s、release-openwrt、build-mobile、publish-web-ota、test-webapp-reusable）里拉 k2 子模块的步骤。
4. 把 k2 的 `release.yml`（打 tag 发版 k2 和 k2s，安装源 dl.k2.52j.me）并进 k2app 的 CI。
5. 改 `Makefile` 的 `init-submodules`、`scripts/check-install-script-sync.sh`、`.labtest.yml`。
6. 改 CLAUDE.md 里“k2 子模块只读”的说法（k2app 根目录和 webapp、scripts、api 等 6 处），换成新的规矩：改 k2 要走 k2 自己的测试。
7. 旧 k2 仓库在 GitHub 上设为 archived，README 写明已迁到 k2app/k2。
8. labtest 全套加完整构建都通过后，交给 David 确认合并。
