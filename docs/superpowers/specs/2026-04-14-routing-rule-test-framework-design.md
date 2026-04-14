# Routing Rule Test Framework — Design Spec

**Date**: 2026-04-14
**Goal**: 三层测试覆盖 14 国路由规则的正确性，无需真实网络环境。

## Problem

用户在某国开启 chnroute/国别规则后，部分 app 的流量路由不正确。例如：中国用户的微信视频通话，部分走了 direct（正确），部分走了 proxy（错误）。当前无法系统性地验证 14 个国家的规则数据和路由决策是否正确，也无法快速诊断哪个环节出了问题。

## Design

### 三层测试架构

| 层 | 测什么 | 方法 | CI? |
|---|--------|------|-----|
| L1 数据准确性 | k2b 里 domain/IP 集合是否包含 golden case 中声明的域名和 IP | 直接调 `BundleSet.MatchDomain()` / `MatchIP()`，不经过 Engine | CI |
| L2 引擎正确性 | `Engine.Match()` 对 golden case 返回正确 Target | 用真实 k2b 构建 `rule.Engine`，逐条 `Match(host)` | CI |
| L3 端到端决策 | 模拟完整路由链路：preset 展开 → RouteEntry 组装 → first-match-wins 匹配 | 构造 `[]RouteConfig`（chnroute 模式：direct 匹配国内 + proxy catch-all），走 Engine 级集成 | CI |
| 扩充校验 | k2b 数据与公开源（v2fly geosite、MaxMint GeoLite2）的交叉比对 | 下载 geosite 数据，逐域名/IP 与 k2b 交叉比对，报告差异 | 本地按需 |

### Golden File 结构

文件位置：`k2/rule/testdata/golden_routes.yaml`

按国家 → 按 app 组织。每个 app 声明域名族和 IP 段的预期路由结果。

```yaml
countries:
  - code: cn
    preset: cn-access
    # 端到端测试用的路由配置：国内 direct，其余 proxy
    route_config:
      - via: direct
        match: {preset: cn-access}
      - via: proxy
        match: {all: true}
    apps:
      - name: wechat
        source: "v2fly geosite:tencent + qcloud"
        domains_direct:
          # qq.com 后缀 — suffix match 覆盖所有子域名
          - qq.com
          - weixin.qq.com
          - channels.weixin.qq.com        # 视频号
          - szextshort.weixin.qq.com      # 视频通话 短连接
          - long.weixin.qq.com            # 长连接
          - minorshort.weixin.qq.com      # 视频通话 辅助
          # 独立域名（不在 qq.com 后缀下）
          - wechat.com
          - wechatpay.cn
          - servicewechat.com             # 小程序
          - wxlivecdn.com                 # 直播 CDN
          - wxcloudrun.com                # 云托管
          - wxgateway.com                 # 网关
          - weixinbridge.com              # JS 桥
          # 腾讯云 CDN（服务微信媒体分发）
          - cdntip.com
          - tcdnlive.com
          - tlivemcdn.com
          - txlivecdn.com
          - tcdnvod.com
          - myqcloud.com
        ips_direct:
          - "109.244.0.0/16"              # 腾讯云
          - "203.205.0.0/16"              # 腾讯核心
          - "183.3.0.0/16"                # 腾讯深圳
          - "112.60.0.0/14"               # 腾讯
          - "101.89.0.0/16"               # 腾讯上海
        domains_proxy: []
        ips_proxy: []

      - name: bilibili
        source: "v2fly geosite:bilibili"
        domains_direct:
          - bilibili.com
          - hdslb.com                     # 静态资源 CDN
          - bilivideo.com                 # 视频 CDN
          - bilivideo.cn                  # 视频 CDN
          - biliapi.com                   # API
          - biliapi.net
          - bilicomic.com
          - im9.com                       # 图片 CDN

      - name: douyin
        source: "v2fly geosite:tiktok (CN portion)"
        domains_direct:
          - douyin.com
          - douyinpic.com
          - douyincdn.com
          - douyinvod.com
          - amemv.com
          - snssdk.com
          - bytecdn.cn
          - bytedance.com
          - bytedance.net
          - byted.org
          - pstatp.com
          - toutiao.com
          - toutiaoimg.com
          - toutiaocdn.com
          - ixigua.com

      - name: taobao-alipay
        source: "v2fly geosite:alibaba"
        domains_direct:
          - taobao.com
          - tmall.com
          - alipay.com
          - alicdn.com
          - aliyun.com
          - aliyuncs.com
          - alibabacloud.com
          - tbcdn.cn
          - mmstat.com
          - tanx.com

      - name: baidu
        source: "v2fly geosite:baidu"
        domains_direct:
          - baidu.com
          - bdstatic.com
          - bdimg.com
          - baidubce.com
          - bcebos.com
          - baiducontent.com

      - name: jd
        source: "v2fly geosite:jd"
        domains_direct:
          - jd.com
          - jd.hk
          - jdcloud.com
          - 360buyimg.com
          - jdpay.com

      - name: weibo
        source: "v2fly geosite:sina"
        domains_direct:
          - weibo.com
          - weibo.cn
          - sinaimg.cn
          - sina.com.cn

      - name: netease
        source: "v2fly geosite:netease"
        domains_direct:
          - 163.com
          - 126.com
          - netease.com
          - music.163.com
          - ntes.com
          - ydstatic.com

      - name: xiaohongshu
        domains_direct:
          - xiaohongshu.com
          - xhscdn.com
          - xhslink.com

      - name: meituan
        domains_direct:
          - meituan.com
          - dianping.com
          - meituan.net

      - name: kuaishou
        domains_direct:
          - kuaishou.com
          - gifshow.com
          - kwai.com
          - yxixy.com

      - name: pinduoduo
        domains_direct:
          - pinduoduo.com
          - yangkeduo.com

      # --- 海外服务（应走 proxy）---
      - name: google
        domains_proxy:
          - google.com
          - googleapis.com
          - gstatic.com
          - youtube.com
          - googlevideo.com
          - ggpht.com
          - googleusercontent.com
        ips_proxy:
          - "8.8.8.0/24"
          - "8.8.4.0/24"

      - name: telegram
        domains_proxy:
          - t.me
          - telegram.org
          - telesco.pe
        ips_proxy:
          - "149.154.160.0/20"
          - "91.108.56.0/22"

      - name: facebook-meta
        domains_proxy:
          - facebook.com
          - fbcdn.net
          - instagram.com
          - whatsapp.com
          - whatsapp.net

      - name: openai
        domains_proxy:
          - openai.com
          - chatgpt.com

  - code: ir
    preset: ir-access
    route_config:
      - via: direct
        match: {preset: ir-access}
      - via: proxy
        match: {all: true}
    apps:
      - name: digikala
        domains_direct: [digikala.com, digistyle.com]
      - name: snapp
        domains_direct: [snapp.ir, snapp.cab, snappfood.ir]
      - name: aparat
        domains_direct: [aparat.com, telewebion.com]
      - name: rubika
        domains_direct: [rubika.ir]
      - name: bale
        domains_direct: [bale.ai]
      - name: divar
        domains_direct: [divar.ir]
      - name: filimo
        domains_direct: [filimo.com]
      - name: namava
        domains_direct: [namava.ir]
      - name: torob
        domains_direct: [torob.com]
      - name: cafe-bazaar
        domains_direct: [cafebazaar.ir, cafe-bazaar.ir]
      # 海外服务
      - name: google
        domains_proxy: [google.com, youtube.com, googleapis.com]
      - name: meta
        domains_proxy: [facebook.com, instagram.com, whatsapp.com]

  - code: ru
    preset: ru-access
    route_config:
      - via: direct
        match: {preset: ru-access}
      - via: proxy
        match: {all: true}
    apps:
      - name: vk
        domains_direct: [vk.com, vkontakte.ru, vk.me, userapi.com, vkuservideo.net, vkuser.net]
      - name: yandex
        domains_direct: [yandex.ru, yandex.net, yandex.com, yastatic.net, ya.ru]
      - name: mail-ru
        domains_direct: [mail.ru, mycdn.me, imgsmail.ru, list.ru]
      - name: ozon
        domains_direct: [ozon.ru, ozon.st]
      - name: wildberries
        domains_direct: [wildberries.ru, wbstatic.net, wb.ru]
      - name: sber
        domains_direct: [sberbank.ru, sber.ru, online.sberbank.ru]
      - name: rutube
        domains_direct: [rutube.ru]
      - name: avito
        domains_direct: [avito.ru, avito.st]
      - name: tinkoff
        domains_direct: [tinkoff.ru, tcsbank.ru]
      - name: gosuslugi
        domains_direct: [gosuslugi.ru, esia.gosuslugi.ru]
      - name: 2gis
        domains_direct: [2gis.ru, 2gis.com]
      # 海外服务
      - name: google
        domains_proxy: [google.com, youtube.com]

  - code: tr
    preset: tr-access
    route_config:
      - via: direct
        match: {preset: tr-access}
      - via: proxy
        match: {all: true}
    apps:
      - name: trendyol
        domains_direct: [trendyol.com, ty.gl]
      - name: hepsiburada
        domains_direct: [hepsiburada.com]
      - name: sahibinden
        domains_direct: [sahibinden.com]
      - name: n11
        domains_direct: [n11.com]
      - name: getir
        domains_direct: [getir.com]
      - name: yemeksepeti
        domains_direct: [yemeksepeti.com]
      - name: papara
        domains_direct: [papara.com]
      - name: bip
        domains_direct: [bip.com, bip.ai]

  - code: pk
    preset: pk-access
    route_config:
      - via: direct
        match: {preset: pk-access}
      - via: proxy
        match: {all: true}
    apps:
      - name: jazzcash
        domains_direct: [jazzcash.com.pk]
      - name: easypaisa
        domains_direct: [easypaisa.com.pk]
      - name: daraz
        domains_direct: [daraz.pk]
      - name: bykea
        domains_direct: [bykea.com]
      - name: zameen
        domains_direct: [zameen.com]

  - code: vn
    preset: vn-access
    route_config:
      - via: direct
        match: {preset: vn-access}
      - via: proxy
        match: {all: true}
    apps:
      - name: zalo
        domains_direct: [zalo.me, zalo.vn, zaloapp.com, zalopay.vn]
      - name: momo
        domains_direct: [momo.vn]
      - name: shopee-vn
        domains_direct: [shopee.vn]
      - name: tiki
        domains_direct: [tiki.vn]
      - name: fpt-play
        domains_direct: [fptplay.vn, fpt.vn]
      - name: zing
        domains_direct: [zing.vn, mp3.zing.vn]
      - name: vietcombank
        domains_direct: [vietcombank.com.vn]

  - code: mm
    preset: mm-access
    route_config:
      - via: direct
        match: {preset: mm-access}
      - via: proxy
        match: {all: true}
    apps:
      - name: kbzpay
        domains_direct: [kbzpay.com]
      - name: wavemoney
        domains_direct: [wavemoney.io, wavemoney.com.mm]
      - name: mpt
        domains_direct: [mpt.com.mm]

  - code: eg
    preset: eg-access
    route_config:
      - via: direct
        match: {preset: eg-access}
      - via: proxy
        match: {all: true}
    apps:
      - name: fawry
        domains_direct: [fawry.com, fawrypay.com]
      - name: talabat
        domains_direct: [talabat.com]
      - name: shahid
        domains_direct: [shahid.mbc.net, shahid.net]
      - name: jumia
        domains_direct: [jumia.com.eg]

  - code: id
    preset: id-access
    route_config:
      - via: direct
        match: {preset: id-access}
      - via: proxy
        match: {all: true}
    apps:
      - name: gojek
        domains_direct: [gojek.com, gopay.co.id, go-jek.com]
      - name: tokopedia
        domains_direct: [tokopedia.com, tokopedia.net]
      - name: shopee-id
        domains_direct: [shopee.co.id]
      - name: dana
        domains_direct: [dana.id]
      - name: traveloka
        domains_direct: [traveloka.com]
      - name: vidio
        domains_direct: [vidio.com]
      - name: bukalapak
        domains_direct: [bukalapak.com]

  - code: sa
    preset: sa-access
    route_config:
      - via: direct
        match: {preset: sa-access}
      - via: proxy
        match: {all: true}
    apps:
      - name: stc-pay
        domains_direct: [stcpay.com.sa, stc.com.sa]
      - name: absher
        domains_direct: [absher.sa]
      - name: tawakkalna
        domains_direct: [tawakkalna.sdaia.gov.sa]
      - name: jahez
        domains_direct: [jahez.net]
      - name: noon-sa
        domains_direct: [noon.com]
      - name: hungerstation
        domains_direct: [hungerstation.com]

  - code: ae
    preset: ae-access
    route_config:
      - via: direct
        match: {preset: ae-access}
      - via: proxy
        match: {all: true}
    apps:
      - name: careem
        domains_direct: [careem.com]
      - name: noon-ae
        domains_direct: [noon.com]
      - name: talabat-ae
        domains_direct: [talabat.com]
      - name: botim
        domains_direct: [botim.me]
      - name: alhosn
        domains_direct: [alhosn.ae]

  - code: th
    preset: th-access
    route_config:
      - via: direct
        match: {preset: th-access}
      - via: proxy
        match: {all: true}
    apps:
      - name: line
        domains_direct: [line.me, line-scdn.net, line-apps.com, linecorp.com, naver.jp]
      - name: truemoney
        domains_direct: [truemoney.com, truemoveh.com]
      - name: shopee-th
        domains_direct: [shopee.co.th]
      - name: grab-th
        domains_direct: [grab.com]
      - name: scb
        domains_direct: [scb.co.th]
      - name: kbank
        domains_direct: [kasikornbank.com]

  - code: bd
    preset: bd-access
    route_config:
      - via: direct
        match: {preset: bd-access}
      - via: proxy
        match: {all: true}
    apps:
      - name: bkash
        domains_direct: [bkash.com]
      - name: nagad
        domains_direct: [nagad.com.bd]
      - name: pathao
        domains_direct: [pathao.com]
      - name: daraz-bd
        domains_direct: [daraz.com.bd]
      - name: grameenphone
        domains_direct: [grameenphone.com, gp.com.bd]

  - code: by
    preset: by-access
    route_config:
      - via: direct
        match: {preset: by-access}
      - via: proxy
        match: {all: true}
    apps:
      - name: yandex-by
        domains_direct: [yandex.by]
      - name: wildberries-by
        domains_direct: [wildberries.by]
      - name: kufar
        domains_direct: [kufar.by]
      - name: onliner
        domains_direct: [onliner.by]
      - name: 21vek
        domains_direct: [21vek.by]
      - name: belarusbank
        domains_direct: [belarusbank.by]
```

### 测试实现

所有测试文件放在 `k2/rule/` 包内。

#### 文件 1：`k2/rule/golden_test.go` — L1 + L2 + L3

```go
//go:build !short

// TestGolden_L1_DataAccuracy — 直接查 BundleSet，验证数据覆盖
// TestGolden_L2_EngineMatch — 构建 Engine，验证 Match() 返回正确 Target
// TestGolden_L3_EndToEnd — 模拟完整 route config，验证端到端决策
```

工作流程：
1. 解析 `testdata/golden_routes.yaml`
2. 下载所有需要的 k2b 文件到 `t.TempDir()`（复用现有 `downloadFile` helper）
3. 每一层独立跑所有 country × app × case

每个 case 产生独立的 `t.Run` subtest，格式：
```
TestGolden_L2_EngineMatch/cn/wechat/qq.com=direct
TestGolden_L2_EngineMatch/cn/wechat/wxlivecdn.com=direct
TestGolden_L2_EngineMatch/cn/google/google.com=proxy
```

失败输出：
```
FAIL: cn/wechat/wxlivecdn.com — expected direct, got proxy
  L1 check: wxlivecdn.com NOT in cn-sites (data gap)
```

#### 文件 2：`k2/rule/diagnose_test.go` — 诊断工具

```go
//go:build !short

// TestDiagnose — 单域名/IP 诊断工具
// 用法: go test -run TestDiagnose -v -args -host=mmtcdn.cn
```

接受 `-host` flag，输出：
1. 该 host 在每个 BundleSet 中的匹配结果（MATCH / NO MATCH / n/a）
2. 在每个国家 preset 下的最终路由决策
3. 如果该域名在 v2fly geosite 中存在但 k2b 中不存在，给出 warning

#### 文件 3：`k2/rule/audit_test.go` — 公开数据源交叉比对

```go
//go:build audit

// TestAudit_GeositeCrossCheck — 下载 v2fly geosite 数据，
// 逐域名与 k2b 交叉比对，输出差异报告
```

本地运行：`go test -tags audit -run TestAudit -v -timeout 120s`

工作流程：
1. 下载 v2fly domain-list-community 的相关数据文件（tencent、bilibili、alibaba 等）
2. 解析为域名列表
3. 对每个域名调 `BundleSet.MatchDomain()`
4. 输出 "在 geosite 里但不在 k2b 里" 的域名列表

不是断言失败 — 而是生成报告。用于发现 k2b 数据的盲区，指导 k2-rules 仓库更新。

### Golden File 维护流程

1. **新增国家 app**：在 YAML 中添加 app 条目 + 域名列表
2. **用户反馈路由问题**：先跑 `TestDiagnose -args -host=xxx`，定位根因，然后补充 golden case
3. **k2-rules 更新后**：CI 自动跑 L1-L3，如果有域名新增到 k2b，golden file 不需要改（多覆盖不是问题）；如果有域名从 k2b 中移除，golden test 会 fail
4. **定期审计**：跑 `TestAudit`，从 geosite diff 中发现新增域名，补充到 golden file

### Target 映射约定

Golden file 中 `expect: direct` 映射到 `Target(0)`，`expect: proxy` 映射到 `Target(2)`。这与 `rule.Target` 的约定一致（0=direct, 1=reject, 2+=wire outbounds）。L3 端到端测试构造两条 route：第一条 `via: direct` 对应 `Target(0)` + preset match，第二条 `via: proxy` 对应 `Target(2)` + catch-all。Fallback 为 `Target(0)`（direct），与生产行为一致。

### 不做的事

- **不做真实网络测试** — 不需要从中国/伊朗发起真实连接
- **不做 DNS 解析验证** — 只测 domain/IP 到 target 的映射，不测 DNS 返回的 IP 是否在 geoip 里（那是 DNS 层的问题）
- **不做 process/package name 测试** — 那是引擎测试已覆盖的（`engine_test.go`），golden file 聚焦 host-based 路由
- **不为每个国家维护 geosite 全量拉取** — audit 工具只拉 CN（最成熟）+ 用户反馈多的国家

### 文件清单

| 文件 | 新增/修改 | 用途 |
|------|----------|------|
| `k2/rule/testdata/golden_routes.yaml` | 新增 | Golden file（14 国 × top apps） |
| `k2/rule/golden_test.go` | 新增 | L1 + L2 + L3 三层断言测试 |
| `k2/rule/diagnose_test.go` | 新增 | 单 host 诊断工具 |
| `k2/rule/audit_test.go` | 新增 | 公开数据源交叉比对 |
