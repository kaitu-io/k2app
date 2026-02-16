package cloudprovider

// Region represents a unified region with multi-language support
type Region struct {
	Slug      string            `json:"slug"`      // URL-friendly identifier: "us-virginia"
	NameEN    string            `json:"nameEn"`    // English name: "US East (Virginia)"
	NameZH    string            `json:"nameZh"`    // Chinese name: "美国东部（弗吉尼亚）"
	Country   string            `json:"country"`   // Country code: "US"
	Providers map[string]string `json:"-"`         // Provider-specific mapping: {"aws_lightsail": "us-east-1"}
}

// AllRegions is the unified region registry
// Comprehensive list covering all major cloud providers' supported regions
var AllRegions = []Region{
	// ============================================================
	// North America
	// ============================================================
	{Slug: "us-virginia", NameEN: "US East (Virginia)", NameZH: "美国东部（弗吉尼亚）", Country: "US",
		Providers: map[string]string{ProviderAWSLightsail: "us-east-1", ProviderAlibabaSWAS: "us-east-1"}},
	{Slug: "us-ohio", NameEN: "US East (Ohio)", NameZH: "美国东部（俄亥俄）", Country: "US",
		Providers: map[string]string{ProviderAWSLightsail: "us-east-2"}},
	{Slug: "us-oregon", NameEN: "US West (Oregon)", NameZH: "美国西部（俄勒冈）", Country: "US",
		Providers: map[string]string{ProviderAWSLightsail: "us-west-2"}},
	{Slug: "us-siliconvalley", NameEN: "US West (Silicon Valley)", NameZH: "美国西部（硅谷）", Country: "US",
		Providers: map[string]string{ProviderAlibabaSWAS: "us-west-1", ProviderTencentLighthouse: "na-siliconvalley"}},
	{Slug: "ca-central", NameEN: "Canada (Central)", NameZH: "加拿大（中部）", Country: "CA",
		Providers: map[string]string{ProviderAWSLightsail: "ca-central-1"}},

	// ============================================================
	// Europe
	// ============================================================
	{Slug: "eu-ireland", NameEN: "Europe (Ireland)", NameZH: "欧洲（爱尔兰）", Country: "IE",
		Providers: map[string]string{ProviderAWSLightsail: "eu-west-1"}},
	{Slug: "eu-london", NameEN: "Europe (London)", NameZH: "欧洲（伦敦）", Country: "GB",
		Providers: map[string]string{ProviderAWSLightsail: "eu-west-2", ProviderAlibabaSWAS: "eu-west-1"}},
	{Slug: "eu-paris", NameEN: "Europe (Paris)", NameZH: "欧洲（巴黎）", Country: "FR",
		Providers: map[string]string{ProviderAWSLightsail: "eu-west-3"}},
	{Slug: "eu-frankfurt", NameEN: "Europe (Frankfurt)", NameZH: "欧洲（法兰克福）", Country: "DE",
		Providers: map[string]string{ProviderAWSLightsail: "eu-central-1", ProviderAlibabaSWAS: "eu-central-1", ProviderTencentLighthouse: "eu-frankfurt"}},
	{Slug: "eu-stockholm", NameEN: "Europe (Stockholm)", NameZH: "欧洲（斯德哥尔摩）", Country: "SE",
		Providers: map[string]string{ProviderAWSLightsail: "eu-north-1"}},

	// ============================================================
	// Middle East (UAE Critical Requirement)
	// ============================================================
	{Slug: "me-dubai", NameEN: "Middle East (UAE)", NameZH: "中东（阿联酋）", Country: "AE",
		Providers: map[string]string{ProviderAlibabaSWAS: "me-east-1"}},
	{Slug: "me-riyadh", NameEN: "Middle East (Saudi Arabia)", NameZH: "中东（沙特阿拉伯）", Country: "SA",
		Providers: map[string]string{ProviderAlibabaSWAS: "me-central-1"}},

	// ============================================================
	// Asia Pacific
	// ============================================================
	{Slug: "ap-tokyo", NameEN: "Asia Pacific (Tokyo)", NameZH: "亚太（东京）", Country: "JP",
		Providers: map[string]string{ProviderAWSLightsail: "ap-northeast-1", ProviderAlibabaSWAS: "ap-northeast-1", ProviderTencentLighthouse: "ap-tokyo"}},
	{Slug: "ap-seoul", NameEN: "Asia Pacific (Seoul)", NameZH: "亚太（首尔）", Country: "KR",
		Providers: map[string]string{ProviderAWSLightsail: "ap-northeast-2", ProviderAlibabaSWAS: "ap-northeast-2", ProviderTencentLighthouse: "ap-seoul"}},
	{Slug: "ap-singapore", NameEN: "Asia Pacific (Singapore)", NameZH: "亚太（新加坡）", Country: "SG",
		Providers: map[string]string{ProviderAWSLightsail: "ap-southeast-1", ProviderAlibabaSWAS: "ap-southeast-1", ProviderTencentLighthouse: "ap-singapore"}},
	{Slug: "ap-sydney", NameEN: "Asia Pacific (Sydney)", NameZH: "亚太（悉尼）", Country: "AU",
		Providers: map[string]string{ProviderAWSLightsail: "ap-southeast-2", ProviderAlibabaSWAS: "ap-southeast-2"}},
	{Slug: "ap-jakarta", NameEN: "Asia Pacific (Jakarta)", NameZH: "亚太（雅加达）", Country: "ID",
		Providers: map[string]string{ProviderAWSLightsail: "ap-southeast-3", ProviderAlibabaSWAS: "ap-southeast-5", ProviderTencentLighthouse: "ap-jakarta"}},
	{Slug: "ap-mumbai", NameEN: "Asia Pacific (Mumbai)", NameZH: "亚太（孟买）", Country: "IN",
		Providers: map[string]string{ProviderAWSLightsail: "ap-south-1", ProviderAlibabaSWAS: "ap-south-1", ProviderTencentLighthouse: "ap-mumbai"}},
	{Slug: "ap-bangkok", NameEN: "Asia Pacific (Bangkok)", NameZH: "亚太（曼谷）", Country: "TH",
		Providers: map[string]string{ProviderAlibabaSWAS: "ap-southeast-7", ProviderTencentLighthouse: "ap-bangkok"}},
	{Slug: "ap-kualalumpur", NameEN: "Asia Pacific (Kuala Lumpur)", NameZH: "亚太（吉隆坡）", Country: "MY",
		Providers: map[string]string{ProviderAlibabaSWAS: "ap-southeast-3"}},
	{Slug: "ap-manila", NameEN: "Asia Pacific (Manila)", NameZH: "亚太（马尼拉）", Country: "PH",
		Providers: map[string]string{ProviderAlibabaSWAS: "ap-southeast-6"}},

	// ============================================================
	// Greater China - International (Alibaba/Tencent)
	// ============================================================
	{Slug: "cn-hongkong", NameEN: "Hong Kong", NameZH: "香港", Country: "HK",
		Providers: map[string]string{ProviderAWSLightsail: "ap-east-1", ProviderAlibabaSWAS: "cn-hongkong", ProviderAliyunSWAS: "cn-hongkong", ProviderTencentLighthouse: "ap-hongkong"}},
	{Slug: "cn-taiwan", NameEN: "Taiwan (Taipei)", NameZH: "台湾（台北）", Country: "TW",
		Providers: map[string]string{ProviderAlibabaSWAS: "ap-northeast-3"}},

	// ============================================================
	// Greater China - Domestic (Aliyun/QCloud)
	// ============================================================
	{Slug: "cn-shanghai", NameEN: "China (Shanghai)", NameZH: "中国（上海）", Country: "CN",
		Providers: map[string]string{ProviderAliyunSWAS: "cn-shanghai", ProviderQCloudLighthouse: "ap-shanghai"}},
	{Slug: "cn-beijing", NameEN: "China (Beijing)", NameZH: "中国（北京）", Country: "CN",
		Providers: map[string]string{ProviderAliyunSWAS: "cn-beijing", ProviderQCloudLighthouse: "ap-beijing"}},
	{Slug: "cn-shenzhen", NameEN: "China (Shenzhen)", NameZH: "中国（深圳）", Country: "CN",
		Providers: map[string]string{ProviderAliyunSWAS: "cn-shenzhen"}},
	{Slug: "cn-hangzhou", NameEN: "China (Hangzhou)", NameZH: "中国（杭州）", Country: "CN",
		Providers: map[string]string{ProviderAliyunSWAS: "cn-hangzhou"}},
	{Slug: "cn-guangzhou", NameEN: "China (Guangzhou)", NameZH: "中国（广州）", Country: "CN",
		Providers: map[string]string{ProviderQCloudLighthouse: "ap-guangzhou"}},
	{Slug: "cn-chengdu", NameEN: "China (Chengdu)", NameZH: "中国（成都）", Country: "CN",
		Providers: map[string]string{ProviderQCloudLighthouse: "ap-chengdu"}},
	{Slug: "cn-nanjing", NameEN: "China (Nanjing)", NameZH: "中国（南京）", Country: "CN",
		Providers: map[string]string{ProviderQCloudLighthouse: "ap-nanjing"}},

	// ============================================================
	// South America
	// ============================================================
	{Slug: "sa-saopaulo", NameEN: "South America (São Paulo)", NameZH: "南美（圣保罗）", Country: "BR",
		Providers: map[string]string{ProviderAWSLightsail: "sa-east-1"}},
}

// regionBySlug caches regions by slug for fast lookup
var regionBySlug map[string]*Region

// regionByProviderID caches regions by provider+region for fast lookup
var regionByProviderID map[string]map[string]*Region

func init() {
	regionBySlug = make(map[string]*Region)
	regionByProviderID = make(map[string]map[string]*Region)

	for i := range AllRegions {
		r := &AllRegions[i]
		regionBySlug[r.Slug] = r

		for provider, providerRegion := range r.Providers {
			if regionByProviderID[provider] == nil {
				regionByProviderID[provider] = make(map[string]*Region)
			}
			regionByProviderID[provider][providerRegion] = r
		}
	}
}

// GetRegionBySlug returns a region by its unified slug
func GetRegionBySlug(slug string) *Region {
	return regionBySlug[slug]
}

// GetRegionByProviderID returns a region by provider-specific region ID
func GetRegionByProviderID(provider, providerRegion string) *Region {
	if m, ok := regionByProviderID[provider]; ok {
		return m[providerRegion]
	}
	return nil
}

// GetProviderRegion returns the provider-specific region ID for a unified slug
func GetProviderRegion(slug, provider string) string {
	r := regionBySlug[slug]
	if r == nil {
		return ""
	}
	return r.Providers[provider]
}

// ListRegionsForProvider returns all regions available for a given provider
func ListRegionsForProvider(provider string) []Region {
	var regions []Region
	for _, r := range AllRegions {
		if _, ok := r.Providers[provider]; ok {
			regions = append(regions, r)
		}
	}
	return regions
}

// GetAWSLightsailRegions returns all AWS Lightsail region IDs
func GetAWSLightsailRegions() []string {
	var regions []string
	for _, r := range AllRegions {
		if awsRegion, ok := r.Providers[ProviderAWSLightsail]; ok {
			regions = append(regions, awsRegion)
		}
	}
	return regions
}

// GetAliyunSWASRegions returns all Aliyun SWAS domestic region IDs
func GetAliyunSWASRegions() []string {
	var regions []string
	for _, r := range AllRegions {
		if aliyunRegion, ok := r.Providers[ProviderAliyunSWAS]; ok {
			regions = append(regions, aliyunRegion)
		}
	}
	return regions
}

// GetAlibabaSWASRegions returns all Alibaba SWAS international region IDs
func GetAlibabaSWASRegions() []string {
	var regions []string
	for _, r := range AllRegions {
		if alibabaRegion, ok := r.Providers[ProviderAlibabaSWAS]; ok {
			regions = append(regions, alibabaRegion)
		}
	}
	return regions
}

// GetTencentLighthouseRegions returns all Tencent Lighthouse international region IDs
func GetTencentLighthouseRegions() []string {
	var regions []string
	for _, r := range AllRegions {
		if tencentRegion, ok := r.Providers[ProviderTencentLighthouse]; ok {
			regions = append(regions, tencentRegion)
		}
	}
	return regions
}

// GetQCloudLighthouseRegions returns all QCloud Lighthouse domestic region IDs
func GetQCloudLighthouseRegions() []string {
	var regions []string
	for _, r := range AllRegions {
		if qcloudRegion, ok := r.Providers[ProviderQCloudLighthouse]; ok {
			regions = append(regions, qcloudRegion)
		}
	}
	return regions
}
