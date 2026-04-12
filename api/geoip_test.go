package center

import (
	"context"
	"net/netip"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// newStubLoader 返回一个返回固定 CIDR 列表的测试加载器
// 每个 key 是 cc，value 是该国家包含的 CIDR 列表
func newStubLoader(data map[string][]string) geoipLoaderFunc {
	return func(_ context.Context, cc string) ([]netip.Prefix, error) {
		lines := data[strings.ToLower(cc)]
		var out []netip.Prefix
		for _, line := range lines {
			p, err := netip.ParsePrefix(line)
			if err != nil {
				continue
			}
			out = append(out, p)
		}
		return out, nil
	}
}

// geoipStubData 覆盖 known-IP 测试需要命中的前缀
// 114.114.114.114 在 114.114.114.0/24
// 77.88.8.8 (Yandex DNS) 在 77.88.8.0/24
// 185.143.233.120 (Iran) 在 185.143.232.0/22
// 8.8.8.8 不在任何目标国家
var geoipStubData = map[string][]string{
	"cn": {"114.114.114.0/24", "223.5.5.0/24"},
	"ru": {"77.88.8.0/24", "213.180.193.0/24"},
	"ir": {"185.143.232.0/22", "2.176.0.0/12"},
	"tr": {"5.2.64.0/20"},
	"pk": {"101.50.0.0/16"},
	"vn": {"14.160.0.0/11"},
	"mm": {"103.10.92.0/22"},
	"eg": {"41.32.0.0/11"},
	"id": {"36.64.0.0/11"},
	"sa": {"5.42.192.0/19"},
	"ae": {"5.30.0.0/15"},
	"th": {"1.0.128.0/17"},
	"bd": {"103.7.52.0/22"},
	"by": {"37.17.184.0/22"},
}

func installStubGeoIP(t *testing.T) {
	t.Helper()
	restore := SetGeoIPLoaderForTest(newStubLoader(geoipStubData))
	ResetGeoIPForTest()
	// 直接调用内部 reload，避免 sync.Once + 后台 goroutine
	require.NoError(t, reloadGeoIP(context.Background()))
	t.Cleanup(func() {
		restore()
		ResetGeoIPForTest()
	})
}

func TestLookupCountry_KnownIPs(t *testing.T) {
	installStubGeoIP(t)

	cases := []struct {
		name string
		ip   string
		want string
	}{
		{"china 114 DNS", "114.114.114.114", "cn"},
		{"yandex DNS russia", "77.88.8.8", "ru"},
		{"iran 185.143.233.120", "185.143.233.120", "ir"},
		{"google DNS outside target list", "8.8.8.8", ""},
		{"cloudflare outside target list", "1.1.1.1", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			addr, err := netip.ParseAddr(tc.ip)
			require.NoError(t, err)
			got := LookupCountry(addr)
			assert.Equal(t, tc.want, got)
		})
	}
}

func TestLookupCountry_PrivateIPs(t *testing.T) {
	installStubGeoIP(t)

	cases := []string{
		"192.168.1.1",
		"10.0.0.1",
		"172.16.5.10",
		"127.0.0.1",
		"169.254.1.1", // link-local
		"0.0.0.0",
		"::1",
		"fe80::1",
	}
	for _, ip := range cases {
		t.Run(ip, func(t *testing.T) {
			addr, err := netip.ParseAddr(ip)
			require.NoError(t, err)
			assert.Equal(t, "", LookupCountry(addr))
		})
	}
}

func TestLookupCountry_Malformed(t *testing.T) {
	installStubGeoIP(t)

	// Zero value Addr
	assert.Equal(t, "", LookupCountry(netip.Addr{}))
}

func TestLookupCountry_StoreNotInitialized(t *testing.T) {
	ResetGeoIPForTest()
	addr, _ := netip.ParseAddr("114.114.114.114")
	assert.Equal(t, "", LookupCountry(addr))
}

func TestSuggestedProfileForCountry(t *testing.T) {
	cases := []struct {
		cc   string
		want string
	}{
		{"cn", "cnroute"},
		{"ir", "iroute"},
		{"ru", "ruroute"},
		{"tr", "troute"},
		{"pk", "pkroute"},
		{"vn", "vnroute"},
		{"mm", "mmroute"},
		{"eg", "egroute"},
		{"id", "idroute"},
		{"sa", "saroute"},
		{"ae", "aeroute"},
		{"th", "throute"},
		{"bd", "bdroute"},
		{"by", "byroute"},
		{"", "global"},
		{"us", "global"},
		{"jp", "global"},
		{"CN", "cnroute"}, // case-insensitive
		{" ir ", "iroute"},
	}
	for _, tc := range cases {
		t.Run("cc="+tc.cc, func(t *testing.T) {
			assert.Equal(t, tc.want, SuggestedProfileForCountry(tc.cc))
		})
	}
}

func TestParsePrefixStream(t *testing.T) {
	input := `# comment line
1.2.3.0/24
  10.0.0.0/8
4.5.6.0/24 # inline comment

not-a-cidr
2001:db8::/32
`
	prefixes, err := parsePrefixStream(strings.NewReader(input))
	require.NoError(t, err)
	got := make([]string, len(prefixes))
	for i, p := range prefixes {
		got[i] = p.String()
	}
	assert.Equal(t, []string{
		"1.2.3.0/24",
		"10.0.0.0/8",
		"4.5.6.0/24",
		"2001:db8::/32",
	}, got)
}

func TestLookupCountry_IPv6(t *testing.T) {
	// Install a small v6 stub
	restore := SetGeoIPLoaderForTest(newStubLoader(map[string][]string{
		"cn": {"2408::/20"}, // China Mobile IPv6 block (illustrative)
		"ru": {"2a02:6b8::/32"},
	}))
	defer restore()
	ResetGeoIPForTest()
	require.NoError(t, reloadGeoIP(context.Background()))
	defer ResetGeoIPForTest()

	addr1, _ := netip.ParseAddr("2408:0800::1")
	assert.Equal(t, "cn", LookupCountry(addr1))

	addr2, _ := netip.ParseAddr("2a02:6b8::1")
	assert.Equal(t, "ru", LookupCountry(addr2))

	addr3, _ := netip.ParseAddr("2001:4860:4860::8888") // Google DNS
	assert.Equal(t, "", LookupCountry(addr3))
}

func TestBuildDataUser_IncludesCountryAndProfile(t *testing.T) {
	installStubGeoIP(t)

	u := &User{
		ID:                  42,
		UUID:                "user-42",
		Language:            "en-US",
		RegistrationCountry: "ir",
		CurrentCountry:      "ir",
	}
	data := buildDataUserWithDevice(u, nil)
	require.NotNil(t, data)
	assert.Equal(t, "ir", data.CurrentCountry)
	assert.Equal(t, "ir", data.RegistrationCountry)
	assert.Equal(t, "iroute", data.SuggestedProfile)

	// User with empty country gets global fallback
	u2 := &User{ID: 43, UUID: "user-43", Language: "en-US"}
	data2 := buildDataUserWithDevice(u2, nil)
	assert.Equal(t, "", data2.CurrentCountry)
	assert.Equal(t, "global", data2.SuggestedProfile)
}
