# Routing Rule Test Framework Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Three-layer test coverage (data accuracy, engine correctness, end-to-end decisions) for 14-country VPN routing rules, plus a diagnostic tool and geosite audit.

**Architecture:** Golden YAML file declares per-country per-app test cases. Tests download real k2b bundles and verify at three layers: L1 BundleSet match, L2 Engine.Match(), L3 full route-config assembly. Diagnose tool and geosite audit are separate test files.

**Tech Stack:** Go test, `gopkg.in/yaml.v3` (already in go.mod), `k2/rule` package APIs (BundleSet, Engine, Index, Load, ExpandPreset)

---

## File Structure

| File | Responsibility |
|------|---------------|
| `k2/rule/testdata/golden_routes.yaml` | Golden test data: 14 countries × top apps with expected route decisions |
| `k2/rule/golden_test.go` | L1 + L2 + L3 tests: parse YAML, download k2b, assert at each layer |
| `k2/rule/diagnose_test.go` | Single-host diagnostic tool: `-host` flag, per-set match report |
| `k2/rule/audit_test.go` | Geosite cross-check: download v2fly data, compare with k2b coverage |

All files are in `package rule_test` (external test package), consistent with `integration_test.go`. All use `//go:build !short` (or `//go:build audit` for audit) to skip in `-short` mode.

**Key dependency:** Tests reuse the `downloadFile` helper from `integration_test.go` and the `resolveNames` helper. Since those are in `_test.go` files in the same package, they're available.

---

### Task 1: Create golden_routes.yaml

**Files:**
- Create: `k2/rule/testdata/golden_routes.yaml`

- [ ] **Step 1: Create testdata directory and YAML file**

Create `k2/rule/testdata/golden_routes.yaml` with the full content from the spec. The YAML structure:

```yaml
# Golden route test cases for 14-country routing rules.
# Each country declares apps with expected direct/proxy domains and IPs.
# Used by golden_test.go for L1/L2/L3 assertions.
#
# Structure:
#   countries[].code       — 2-letter country code
#   countries[].preset     — rule preset name (e.g. "cn-access")
#   countries[].apps[].name           — app identifier
#   countries[].apps[].domains_direct — domains expected to route DIRECT
#   countries[].apps[].domains_proxy  — domains expected to route PROXY
#   countries[].apps[].ips_direct     — CIDRs expected to route DIRECT (test first IP)
#   countries[].apps[].ips_proxy      — CIDRs expected to route PROXY (test first IP)

countries:
  - code: cn
    preset: cn-access
    apps:
      - name: wechat
        domains_direct:
          - qq.com
          - weixin.qq.com
          - channels.weixin.qq.com
          - szextshort.weixin.qq.com
          - long.weixin.qq.com
          - minorshort.weixin.qq.com
          - wechat.com
          - wechatpay.cn
          - servicewechat.com
          - wxlivecdn.com
          - wxcloudrun.com
          - wxgateway.com
          - weixinbridge.com
          - cdntip.com
          - tcdnlive.com
          - tlivemcdn.com
          - txlivecdn.com
          - tcdnvod.com
          - myqcloud.com
        ips_direct:
          - "109.244.0.0/16"
          - "203.205.0.0/16"
          - "183.3.0.0/16"
          - "112.60.0.0/14"
          - "101.89.0.0/16"

      - name: bilibili
        domains_direct:
          - bilibili.com
          - hdslb.com
          - bilivideo.com
          - bilivideo.cn
          - biliapi.com
          - biliapi.net
          - bilicomic.com
          - im9.com

      - name: douyin
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
        domains_direct:
          - baidu.com
          - bdstatic.com
          - bdimg.com
          - baidubce.com
          - bcebos.com
          - baiducontent.com

      - name: jd
        domains_direct:
          - jd.com
          - jd.hk
          - jdcloud.com
          - 360buyimg.com
          - jdpay.com

      - name: weibo
        domains_direct:
          - weibo.com
          - weibo.cn
          - sinaimg.cn
          - sina.com.cn

      - name: netease
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
      - name: google
        domains_proxy: [google.com, youtube.com, googleapis.com]
      - name: meta
        domains_proxy: [facebook.com, instagram.com, whatsapp.com]

  - code: ru
    preset: ru-access
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
      - name: google
        domains_proxy: [google.com, youtube.com]

  - code: tr
    preset: tr-access
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
    apps:
      - name: kbzpay
        domains_direct: [kbzpay.com]
      - name: wavemoney
        domains_direct: [wavemoney.io, wavemoney.com.mm]
      - name: mpt
        domains_direct: [mpt.com.mm]

  - code: eg
    preset: eg-access
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

- [ ] **Step 2: Verify YAML is valid**

Run: `cd /Users/david/projects/kaitu-io/k2app && python3 -c "import yaml; yaml.safe_load(open('k2/rule/testdata/golden_routes.yaml'))"`
Expected: No output (valid YAML)

- [ ] **Step 3: Commit**

```bash
cd /Users/david/projects/kaitu-io/k2app
git add k2/rule/testdata/golden_routes.yaml
git commit -m "test(rule): add golden route test data for 14 countries"
```

---

### Task 2: Create golden_test.go — YAML parsing + bundle download helper

**Files:**
- Create: `k2/rule/golden_test.go`

This task creates the YAML data structures, parsing logic, and shared bundle download/setup logic. The actual L1/L2/L3 test functions are stubs that we'll fill in Task 3.

- [ ] **Step 1: Write golden_test.go with types, parser, and bundle setup**

Create `k2/rule/golden_test.go`:

```go
//go:build !short

package rule_test

import (
	"net/netip"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kaitu-io/k2/rule"
	"gopkg.in/yaml.v3"
)

// --- YAML schema ---

type goldenFile struct {
	Countries []goldenCountry `yaml:"countries"`
}

type goldenApp struct {
	Name          string   `yaml:"name"`
	DomainsDirect []string `yaml:"domains_direct"`
	DomainsProxy  []string `yaml:"domains_proxy"`
	IPsDirect     []string `yaml:"ips_direct"`
	IPsProxy      []string `yaml:"ips_proxy"`
}

type goldenCountry struct {
	Code   string      `yaml:"code"`
	Preset string      `yaml:"preset"`
	Apps   []goldenApp `yaml:"apps"`
}

// --- helpers ---

const goldenPath = "testdata/golden_routes.yaml"

func loadGolden(t *testing.T) goldenFile {
	t.Helper()
	data, err := os.ReadFile(goldenPath)
	if err != nil {
		t.Fatalf("read golden file: %v", err)
	}
	var gf goldenFile
	if err := yaml.Unmarshal(data, &gf); err != nil {
		t.Fatalf("parse golden file: %v", err)
	}
	if len(gf.Countries) == 0 {
		t.Fatal("golden file has no countries")
	}
	return gf
}

// downloadBundles downloads all k2b files needed by the golden countries.
// Returns the cache dir path and loaded bundle index.
func downloadBundles(t *testing.T, gf goldenFile) (string, []*rule.Bundle, map[string]*rule.BundleSet) {
	t.Helper()
	dir := t.TempDir()

	// Collect unique bundle files needed across all countries.
	needed := make(map[string]struct{})
	needed["overseas.k2b"] = struct{}{} // always needed for proxy-side matching
	for _, c := range gf.Countries {
		setNames := rule.ExpandPreset(c.Preset)
		for _, name := range setNames {
			files := rule.BundlesForConfig([]rule.MatchConfig{{Names: []string{name}}})
			for _, f := range files {
				needed[f] = struct{}{}
			}
		}
	}

	// Download each bundle.
	for name := range needed {
		url := releaseBase + "/" + name
		path := filepath.Join(dir, name)
		if err := downloadFile(url, path); err != nil {
			t.Fatalf("download %s: %v", name, err)
		}
	}

	// Load all bundles.
	bundles, err := rule.Load(dir)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	idx := rule.Index(bundles)
	return dir, bundles, idx
}

// firstIPInCIDR parses a CIDR string and returns the first usable host IP.
// For "109.244.0.0/16" returns "109.244.0.1" (skip network address).
func firstIPInCIDR(cidr string) (netip.Addr, error) {
	prefix, err := netip.ParsePrefix(cidr)
	if err != nil {
		return netip.Addr{}, err
	}
	addr := prefix.Addr().Next() // skip network address
	return addr, nil
}

// buildEngine constructs a rule.Engine for a country's preset.
// Route config: preset → direct (Target 0), catch-all → proxy (Target 2).
// Fallback: direct (Target 0).
func buildEngine(t *testing.T, preset string, bundles []*rule.Bundle, idx map[string]*rule.BundleSet) *rule.Engine {
	t.Helper()

	// Route 1: preset sets → direct.
	presetNames := rule.ExpandPreset(preset)
	var directSets []*rule.BundleSet
	for _, name := range presetNames {
		if s, ok := idx[name]; ok {
			directSets = append(directSets, s)
		}
	}

	// Route 2: overseas set → proxy (simulates "everything else goes through tunnel").
	var proxySets []*rule.BundleSet
	if s, ok := idx["overseas"]; ok {
		proxySets = append(proxySets, s)
	}

	routes := []rule.RouteEntry{
		{Target: rule.Target(0), Sets: directSets},  // direct
		{Target: rule.Target(2), Sets: proxySets},    // proxy
	}
	return rule.NewEngine(routes, rule.Target(0), bundles, false)
}
```

- [ ] **Step 2: Verify it compiles**

Run: `cd /Users/david/projects/kaitu-io/k2app/k2 && go build ./rule/...`
Expected: No errors. (The file has no test functions yet, just types and helpers.)

Actually since it's a `_test.go` file, use:
Run: `cd /Users/david/projects/kaitu-io/k2app/k2 && go test -short -run NOMATCH ./rule/`
Expected: `ok` (compiles, no tests matched)

- [ ] **Step 3: Commit**

```bash
cd /Users/david/projects/kaitu-io/k2app
git add k2/rule/golden_test.go
git commit -m "test(rule): golden test scaffolding — YAML types, bundle download, engine builder"
```

---

### Task 3: Implement L1, L2, L3 test functions in golden_test.go

**Files:**
- Modify: `k2/rule/golden_test.go`

- [ ] **Step 1: Add L1 test — BundleSet data accuracy**

Append to `k2/rule/golden_test.go`:

```go
func TestGolden_L1_DataAccuracy(t *testing.T) {
	gf := loadGolden(t)
	_, _, idx := downloadBundles(t, gf)

	for _, country := range gf.Countries {
		presetNames := rule.ExpandPreset(country.Preset)

		for _, app := range country.Apps {
			// Check domains_direct: must be in one of the preset's sets.
			for _, domain := range app.DomainsDirect {
				name := country.Code + "/" + app.Name + "/" + domain + "=direct"
				t.Run(name, func(t *testing.T) {
					for _, setName := range presetNames {
						if s, ok := idx[setName]; ok && s.MatchDomain(domain) {
							return // found
						}
					}
					t.Errorf("domain %q not found in any set of preset %s (sets: %v)", domain, country.Preset, presetNames)
				})
			}

			// Check ips_direct: first IP in CIDR must be in geoip set.
			for _, cidr := range app.IPsDirect {
				addr, err := firstIPInCIDR(cidr)
				if err != nil {
					t.Errorf("bad CIDR %q in %s/%s: %v", cidr, country.Code, app.Name, err)
					continue
				}
				name := country.Code + "/" + app.Name + "/" + cidr + "=direct"
				t.Run(name, func(t *testing.T) {
					for _, setName := range presetNames {
						if s, ok := idx[setName]; ok && s.MatchIP(addr) {
							return // found
						}
					}
					t.Errorf("IP %s (from %s) not found in any set of preset %s", addr, cidr, country.Preset)
				})
			}

			// Check domains_proxy: must NOT be in the preset's sets (ensures proxy route wins).
			for _, domain := range app.DomainsProxy {
				name := country.Code + "/" + app.Name + "/" + domain + "=proxy"
				t.Run(name, func(t *testing.T) {
					for _, setName := range presetNames {
						if s, ok := idx[setName]; ok && s.MatchDomain(domain) {
							t.Errorf("domain %q unexpectedly matched set %q in preset %s (should NOT be direct)", domain, setName, country.Preset)
							return
						}
					}
				})
			}

			// Check ips_proxy: first IP must NOT be in geoip set.
			for _, cidr := range app.IPsProxy {
				addr, err := firstIPInCIDR(cidr)
				if err != nil {
					t.Errorf("bad CIDR %q in %s/%s: %v", cidr, country.Code, app.Name, err)
					continue
				}
				name := country.Code + "/" + app.Name + "/" + cidr + "=proxy"
				t.Run(name, func(t *testing.T) {
					for _, setName := range presetNames {
						if s, ok := idx[setName]; ok && s.MatchIP(addr) {
							t.Errorf("IP %s (from %s) unexpectedly matched set %q in preset %s", addr, cidr, setName, country.Preset)
							return
						}
					}
				})
			}
		}
	}
}
```

- [ ] **Step 2: Add L2 test — Engine.Match() correctness**

Append to `k2/rule/golden_test.go`:

```go
func TestGolden_L2_EngineMatch(t *testing.T) {
	gf := loadGolden(t)
	_, bundles, idx := downloadBundles(t, gf)

	for _, country := range gf.Countries {
		eng := buildEngine(t, country.Preset, bundles, idx)
		defer eng.Close()

		for _, app := range country.Apps {
			for _, domain := range app.DomainsDirect {
				name := country.Code + "/" + app.Name + "/" + domain + "=direct"
				t.Run(name, func(t *testing.T) {
					got := eng.Match(domain)
					if got != rule.Target(0) {
						t.Errorf("Engine.Match(%q) = %v, want direct (Target 0)", domain, got)
					}
				})
			}

			for _, domain := range app.DomainsProxy {
				name := country.Code + "/" + app.Name + "/" + domain + "=proxy"
				t.Run(name, func(t *testing.T) {
					got := eng.Match(domain)
					if got != rule.Target(2) {
						t.Errorf("Engine.Match(%q) = %v, want proxy (Target 2)", domain, got)
					}
				})
			}

			for _, cidr := range app.IPsDirect {
				addr, err := firstIPInCIDR(cidr)
				if err != nil {
					continue
				}
				name := country.Code + "/" + app.Name + "/" + cidr + "=direct"
				t.Run(name, func(t *testing.T) {
					got := eng.Match(addr.String())
					if got != rule.Target(0) {
						t.Errorf("Engine.Match(%q) = %v, want direct (Target 0)", addr, got)
					}
				})
			}

			for _, cidr := range app.IPsProxy {
				addr, err := firstIPInCIDR(cidr)
				if err != nil {
					continue
				}
				name := country.Code + "/" + app.Name + "/" + cidr + "=proxy"
				t.Run(name, func(t *testing.T) {
					got := eng.Match(addr.String())
					if got != rule.Target(2) {
						t.Errorf("Engine.Match(%q) = %v, want proxy (Target 2)", addr, got)
					}
				})
			}
		}
	}
}
```

- [ ] **Step 3: Add L3 test — end-to-end route config assembly**

Append to `k2/rule/golden_test.go`:

```go
// TestGolden_L3_EndToEnd tests the full route assembly chain:
// ExpandPreset → resolve sets → build RouteEntry list → Engine.Match().
// This catches preset definition bugs and route ordering issues.
func TestGolden_L3_EndToEnd(t *testing.T) {
	gf := loadGolden(t)
	_, bundles, idx := downloadBundles(t, gf)

	for _, country := range gf.Countries {
		t.Run(country.Code, func(t *testing.T) {
			// Simulate what engine/engine.go buildRouteEntries() does:
			// 1. Expand preset to set names
			// 2. Resolve set names to BundleSet pointers
			// 3. Build route entries: [direct-match, proxy-catchall]
			presetNames := rule.ExpandPreset(country.Preset)
			if presetNames == nil {
				t.Fatalf("ExpandPreset(%q) returned nil — unknown preset", country.Preset)
			}

			var directSets []*rule.BundleSet
			for _, name := range presetNames {
				s, ok := idx[name]
				if !ok {
					t.Fatalf("set %q from preset %q not found in index", name, country.Preset)
				}
				directSets = append(directSets, s)
			}

			// overseas set for proxy route
			overseasSet, ok := idx["overseas"]
			if !ok {
				t.Fatal("overseas set not found in index")
			}

			routes := []rule.RouteEntry{
				{Target: rule.Target(0), Sets: directSets},
				{Target: rule.Target(2), Sets: []*rule.BundleSet{overseasSet}},
			}
			eng := rule.NewEngine(routes, rule.Target(0), bundles, false)
			defer eng.Close()

			// Run all cases through the fully-assembled engine.
			for _, app := range country.Apps {
				for _, domain := range app.DomainsDirect {
					if got := eng.Match(domain); got != rule.Target(0) {
						t.Errorf("%s/%s: Match(%q) = %v, want direct", app.Name, domain, domain, got)
					}
				}
				for _, domain := range app.DomainsProxy {
					if got := eng.Match(domain); got != rule.Target(2) {
						t.Errorf("%s/%s: Match(%q) = %v, want proxy", app.Name, domain, domain, got)
					}
				}
				for _, cidr := range app.IPsDirect {
					addr, err := firstIPInCIDR(cidr)
					if err != nil {
						continue
					}
					if got := eng.Match(addr.String()); got != rule.Target(0) {
						t.Errorf("%s/%s: Match(%q) = %v, want direct", app.Name, cidr, addr, got)
					}
				}
				for _, cidr := range app.IPsProxy {
					addr, err := firstIPInCIDR(cidr)
					if err != nil {
						continue
					}
					if got := eng.Match(addr.String()); got != rule.Target(2) {
						t.Errorf("%s/%s: Match(%q) = %v, want proxy", app.Name, cidr, addr, got)
					}
				}
			}
		})
	}
}
```

- [ ] **Step 4: Run L1/L2/L3 tests (requires network for k2b download)**

Run: `cd /Users/david/projects/kaitu-io/k2app/k2 && go test -v -run 'TestGolden' -timeout 120s ./rule/`
Expected: All three test functions pass. Some individual cases MAY fail if the k2b data doesn't cover certain domains — document any failures.

- [ ] **Step 5: Fix any golden file entries that fail against real k2b data**

If specific domains fail L1 (not in k2b), either:
- Remove them from golden_routes.yaml (they're genuinely not in the rule set)
- Or note them as known gaps with a comment

This is an iterative step — adjust the golden file until the test is green.

- [ ] **Step 6: Commit**

```bash
cd /Users/david/projects/kaitu-io/k2app
git add k2/rule/golden_test.go k2/rule/testdata/golden_routes.yaml
git commit -m "test(rule): L1/L2/L3 golden route tests for 14 countries"
```

---

### Task 4: Create diagnose_test.go — single-host diagnostic tool

**Files:**
- Create: `k2/rule/diagnose_test.go`

- [ ] **Step 1: Write diagnose_test.go**

Create `k2/rule/diagnose_test.go`:

```go
//go:build !short

package rule_test

import (
	"flag"
	"fmt"
	"net/netip"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kaitu-io/k2/rule"
)

var diagnoseHost = flag.String("host", "", "host (domain or IP) to diagnose routing for")

// TestDiagnose is a diagnostic tool for investigating routing decisions.
//
// Usage:
//
//	go test -run TestDiagnose -v -args -host=mmtcdn.cn
//	go test -run TestDiagnose -v -args -host=203.205.147.224
//
// Output: per-set match results and per-country route decisions.
func TestDiagnose(t *testing.T) {
	if *diagnoseHost == "" {
		t.Skip("no -host flag provided; usage: go test -run TestDiagnose -v -args -host=<domain-or-ip>")
	}
	host := strings.ToLower(*diagnoseHost)

	// Download all country bundles.
	dir := t.TempDir()
	allBundles := []string{"overseas.k2b"}
	countries := []string{"ae", "bd", "by", "cn", "eg", "id", "ir", "mm", "pk", "ru", "sa", "th", "tr", "vn"}
	for _, cc := range countries {
		allBundles = append(allBundles, cc+"-direct.k2b")
	}

	for _, name := range allBundles {
		url := releaseBase + "/" + name
		path := filepath.Join(dir, name)
		if err := downloadFile(url, path); err != nil {
			t.Logf("WARN: failed to download %s: %v", name, err)
		}
	}

	bundles, err := rule.Load(dir)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	idx := rule.Index(bundles)

	isIP := false
	var addr netip.Addr
	if a, err := netip.ParseAddr(host); err == nil {
		isIP = true
		addr = a
	}

	// Section 1: Per-set match results.
	fmt.Fprintf(os.Stderr, "\n=== Diagnose: %s ===\n\n", host)
	fmt.Fprintf(os.Stderr, "--- Per-set match results ---\n")

	setNames := sortedKeys(idx)
	for _, name := range setNames {
		s := idx[name]
		if isIP {
			if strings.HasPrefix(name, "geoip-") || name == "overseas" {
				if s.MatchIP(addr) {
					fmt.Fprintf(os.Stderr, "  %-20s MATCH\n", name+":")
				} else {
					fmt.Fprintf(os.Stderr, "  %-20s NO MATCH\n", name+":")
				}
			} else {
				fmt.Fprintf(os.Stderr, "  %-20s n/a (IP query, domain-only set)\n", name+":")
			}
		} else {
			if strings.HasPrefix(name, "geoip-") {
				fmt.Fprintf(os.Stderr, "  %-20s n/a (domain query, IP-only set)\n", name+":")
			} else {
				if s.MatchDomain(host) {
					fmt.Fprintf(os.Stderr, "  %-20s MATCH\n", name+":")
				} else {
					fmt.Fprintf(os.Stderr, "  %-20s NO MATCH\n", name+":")
				}
			}
		}
	}

	// Section 2: Per-country route decisions.
	fmt.Fprintf(os.Stderr, "\n--- Per-country route decisions ---\n")
	presets := map[string]string{
		"cn": "cn-access", "ir": "ir-access", "ru": "ru-access", "tr": "tr-access",
		"pk": "pk-access", "vn": "vn-access", "mm": "mm-access", "eg": "eg-access",
		"id": "id-access", "sa": "sa-access", "ae": "ae-access", "th": "th-access",
		"bd": "bd-access", "by": "by-access",
	}
	for _, cc := range countries {
		preset := presets[cc]
		eng := buildEngine(t, preset, bundles, idx)

		got := eng.Match(host)
		decision := "FALLBACK (direct)"
		switch got {
		case rule.Target(0):
			decision = "DIRECT"
		case rule.Target(2):
			decision = "PROXY"
		}
		fmt.Fprintf(os.Stderr, "  %-5s (%-12s): %s\n", cc, preset, decision)
		eng.Close()
	}

	fmt.Fprintf(os.Stderr, "\n")
}

// sortedKeys returns map keys sorted alphabetically.
func sortedKeys(m map[string]*rule.BundleSet) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	// Simple insertion sort for small maps.
	for i := 1; i < len(keys); i++ {
		for j := i; j > 0 && keys[j] < keys[j-1]; j-- {
			keys[j], keys[j-1] = keys[j-1], keys[j]
		}
	}
	return keys
}
```

- [ ] **Step 2: Verify compilation**

Run: `cd /Users/david/projects/kaitu-io/k2app/k2 && go test -short -run NOMATCH ./rule/`
Expected: `ok` (compiles)

- [ ] **Step 3: Test with a known domain**

Run: `cd /Users/david/projects/kaitu-io/k2app/k2 && go test -run TestDiagnose -v -timeout 120s ./rule/ -args -host=qq.com`
Expected: Output showing `cn-sites: MATCH`, `overseas: NO MATCH`, `cn (cn-access): DIRECT`, other countries show FALLBACK or PROXY.

- [ ] **Step 4: Test with a known IP**

Run: `cd /Users/david/projects/kaitu-io/k2app/k2 && go test -run TestDiagnose -v -timeout 120s ./rule/ -args -host=8.8.8.8`
Expected: Output showing `geoip-cn: NO MATCH`, `overseas: MATCH`, `cn (cn-access): PROXY`.

- [ ] **Step 5: Test skip behavior (no -host flag)**

Run: `cd /Users/david/projects/kaitu-io/k2app/k2 && go test -run TestDiagnose -v -timeout 120s ./rule/`
Expected: `SKIP: no -host flag provided`

- [ ] **Step 6: Commit**

```bash
cd /Users/david/projects/kaitu-io/k2app
git add k2/rule/diagnose_test.go
git commit -m "test(rule): add single-host routing diagnostic tool"
```

---

### Task 5: Create audit_test.go — geosite cross-check

**Files:**
- Create: `k2/rule/audit_test.go`

- [ ] **Step 1: Write audit_test.go**

Create `k2/rule/audit_test.go`:

```go
//go:build audit

package rule_test

import (
	"bufio"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kaitu-io/k2/rule"
)

// geositeBase is the raw URL for v2fly domain-list-community data files.
const geositeBase = "https://raw.githubusercontent.com/v2fly/domain-list-community/master/data/"

// geositeCategories maps k2b set names to v2fly geosite category names.
// Each entry is: k2b-set → []geosite-category.
// A domain in any of the geosite categories should be in the k2b set.
var geositeCategories = map[string][]string{
	"cn-sites": {
		"tencent", "alibaba", "baidu", "bilibili", "bytedance",
		"jd", "netease", "sina", "xiaomi",
	},
	// Add more as needed. IR/RU geosite coverage is less mature.
}

// TestAudit_GeositeCrossCheck downloads v2fly geosite data and compares
// domain coverage with k2b bundles.
//
// Usage: go test -tags audit -run TestAudit -v -timeout 120s ./rule/
//
// This is NOT an assertion test — it generates a coverage report.
// Domains in geosite but missing from k2b are potential data gaps.
func TestAudit_GeositeCrossCheck(t *testing.T) {
	// Download k2b bundles.
	dir := t.TempDir()
	bundleFiles := []string{"cn-direct.k2b", "overseas.k2b"}
	for _, name := range bundleFiles {
		url := releaseBase + "/" + name
		path := filepath.Join(dir, name)
		if err := downloadFile(url, path); err != nil {
			t.Fatalf("download %s: %v", name, err)
		}
	}

	bundles, err := rule.Load(dir)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	idx := rule.Index(bundles)

	for setName, categories := range geositeCategories {
		bs, ok := idx[setName]
		if !ok {
			t.Errorf("set %q not found in bundles", setName)
			continue
		}

		t.Run(setName, func(t *testing.T) {
			var totalDomains, matchCount, missCount int
			var missingDomains []string

			for _, category := range categories {
				domains, err := fetchGeositeDomains(category)
				if err != nil {
					t.Logf("WARN: fetch geosite %q: %v", category, err)
					continue
				}

				for _, domain := range domains {
					totalDomains++
					if bs.MatchDomain(domain) {
						matchCount++
					} else {
						missCount++
						missingDomains = append(missingDomains, fmt.Sprintf("%s (geosite:%s)", domain, category))
					}
				}
			}

			// Report.
			coveragePct := float64(0)
			if totalDomains > 0 {
				coveragePct = float64(matchCount) / float64(totalDomains) * 100
			}
			fmt.Fprintf(os.Stderr, "\n=== Audit: %s ===\n", setName)
			fmt.Fprintf(os.Stderr, "  Total domains checked: %d\n", totalDomains)
			fmt.Fprintf(os.Stderr, "  Matched in k2b:        %d (%.1f%%)\n", matchCount, coveragePct)
			fmt.Fprintf(os.Stderr, "  Missing from k2b:      %d\n", missCount)

			if len(missingDomains) > 0 {
				fmt.Fprintf(os.Stderr, "\n  Missing domains:\n")
				for _, d := range missingDomains {
					fmt.Fprintf(os.Stderr, "    - %s\n", d)
				}
			}
			fmt.Fprintf(os.Stderr, "\n")
		})
	}
}

// fetchGeositeDomains downloads a v2fly geosite category file and extracts
// domain entries. Skips comment lines, @attribute tags, include: directives,
// and regexp: entries.
func fetchGeositeDomains(category string) ([]string, error) {
	url := geositeBase + category
	resp, err := http.Get(url)
	if err != nil {
		return nil, fmt.Errorf("fetch %s: %w", url, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("HTTP %d for %s", resp.StatusCode, url)
	}

	// Limit to 5MB to prevent OOM.
	reader := io.LimitReader(resp.Body, 5<<20)
	scanner := bufio.NewScanner(reader)

	var domains []string
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		// Skip include/regexp directives.
		if strings.HasPrefix(line, "include:") || strings.HasPrefix(line, "regexp:") {
			continue
		}
		// Strip attribute tags (e.g. "@ads", "@cn").
		if idx := strings.IndexByte(line, ' '); idx > 0 {
			line = line[:idx]
		}
		// Strip type prefixes (domain:, full:, keyword:).
		for _, prefix := range []string{"domain:", "full:", "keyword:"} {
			line = strings.TrimPrefix(line, prefix)
		}
		if line == "" {
			continue
		}
		domains = append(domains, strings.ToLower(line))
	}
	return domains, scanner.Err()
}
```

- [ ] **Step 2: Verify compilation (needs -tags audit)**

Run: `cd /Users/david/projects/kaitu-io/k2app/k2 && go test -tags audit -short -run NOMATCH ./rule/`
Expected: `ok`

- [ ] **Step 3: Run the audit**

Run: `cd /Users/david/projects/kaitu-io/k2app/k2 && go test -tags audit -run TestAudit -v -timeout 120s ./rule/`
Expected: Report showing coverage percentage and missing domains. Example output:
```
=== Audit: cn-sites ===
  Total domains checked: 450
  Matched in k2b:        430 (95.6%)
  Missing from k2b:      20
```

- [ ] **Step 4: Commit**

```bash
cd /Users/david/projects/kaitu-io/k2app
git add k2/rule/audit_test.go
git commit -m "test(rule): add geosite cross-check audit tool"
```

---

### Task 6: Final verification and cleanup

**Files:**
- All files created in Tasks 1-5

- [ ] **Step 1: Run all golden tests end-to-end**

Run: `cd /Users/david/projects/kaitu-io/k2app/k2 && go test -v -run 'TestGolden' -timeout 120s ./rule/ 2>&1 | tail -30`
Expected: All L1/L2/L3 tests pass.

- [ ] **Step 2: Run diagnose tool with a tricky domain**

Run: `cd /Users/david/projects/kaitu-io/k2app/k2 && go test -run TestDiagnose -v -timeout 120s ./rule/ -args -host=wxlivecdn.com`
Expected: Shows match results — this domain should be in cn-sites (MATCH) for the WeChat live CDN case.

- [ ] **Step 3: Verify -short mode skips all network tests**

Run: `cd /Users/david/projects/kaitu-io/k2app/k2 && go test -short -v ./rule/ 2>&1 | grep -c 'SKIP'`
Expected: Golden and Diagnose tests are skipped. Existing unit tests still run.

- [ ] **Step 4: Verify audit tag isolation**

Run: `cd /Users/david/projects/kaitu-io/k2app/k2 && go test -v -run TestAudit ./rule/ 2>&1`
Expected: No TestAudit found (because `-tags audit` not specified). This confirms the build tag isolation works.

- [ ] **Step 5: Final commit if any fixes were made**

```bash
cd /Users/david/projects/kaitu-io/k2app
git add -A k2/rule/
git commit -m "test(rule): finalize routing rule test framework"
```
