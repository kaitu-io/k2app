package cloudprovider

import (
	"testing"
)

// TestUAERegionExists verifies that UAE region is properly configured
func TestUAERegionExists(t *testing.T) {
	region := GetRegionBySlug("me-dubai")
	if region == nil {
		t.Fatal("UAE region (me-dubai) must exist")
	}

	if region.Country != "AE" {
		t.Errorf("UAE region country should be AE, got %s", region.Country)
	}

	if region.NameZH != "中东（阿联酋）" {
		t.Errorf("UAE region Chinese name incorrect, got %s", region.NameZH)
	}

	if region.NameEN != "Middle East (UAE)" {
		t.Errorf("UAE region English name incorrect, got %s", region.NameEN)
	}

	// Verify at least one provider supports UAE
	if len(region.Providers) == 0 {
		t.Error("UAE region must have at least one provider")
	}

	// Verify Alibaba SWAS supports UAE (me-east-1)
	alibabaRegion, ok := region.Providers[ProviderAlibabaSWAS]
	if !ok {
		t.Error("Alibaba SWAS should support UAE region")
	}
	if alibabaRegion != "me-east-1" {
		t.Errorf("Alibaba SWAS UAE region ID should be me-east-1, got %s", alibabaRegion)
	}
}

// TestMiddleEastRegions verifies Middle East region coverage
func TestMiddleEastRegions(t *testing.T) {
	tests := []struct {
		slug    string
		country string
		nameZH  string
	}{
		{"me-dubai", "AE", "中东（阿联酋）"},
		{"me-riyadh", "SA", "中东（沙特阿拉伯）"},
	}

	for _, tt := range tests {
		t.Run(tt.slug, func(t *testing.T) {
			region := GetRegionBySlug(tt.slug)
			if region == nil {
				t.Fatalf("Region %s must exist", tt.slug)
			}
			if region.Country != tt.country {
				t.Errorf("Region %s country should be %s, got %s", tt.slug, tt.country, region.Country)
			}
			if region.NameZH != tt.nameZH {
				t.Errorf("Region %s Chinese name incorrect, got %s", tt.slug, region.NameZH)
			}
		})
	}
}

// TestMinimumRegionCount verifies we have comprehensive region coverage
func TestMinimumRegionCount(t *testing.T) {
	minRegions := 25

	if len(AllRegions) < minRegions {
		t.Errorf("Expected at least %d regions, got %d", minRegions, len(AllRegions))
	}
}

// TestAllRegionsHaveProviders verifies each region has at least one provider
func TestAllRegionsHaveProviders(t *testing.T) {
	for _, region := range AllRegions {
		if len(region.Providers) == 0 {
			t.Errorf("Region %s has no providers", region.Slug)
		}
	}
}

// TestRegionLookups verifies region lookup functions work correctly
func TestRegionLookups(t *testing.T) {
	// Test by slug
	region := GetRegionBySlug("ap-tokyo")
	if region == nil {
		t.Fatal("ap-tokyo should exist")
	}

	// Test by provider ID
	regionByProvider := GetRegionByProviderID(ProviderAWSLightsail, "ap-northeast-1")
	if regionByProvider == nil {
		t.Fatal("Should find region by AWS Lightsail ap-northeast-1")
	}
	if regionByProvider.Slug != "ap-tokyo" {
		t.Errorf("AWS ap-northeast-1 should map to ap-tokyo, got %s", regionByProvider.Slug)
	}

	// Test GetProviderRegion
	awsRegion := GetProviderRegion("ap-tokyo", ProviderAWSLightsail)
	if awsRegion != "ap-northeast-1" {
		t.Errorf("ap-tokyo AWS region should be ap-northeast-1, got %s", awsRegion)
	}
}

// TestProviderRegionCoverage verifies each provider has adequate region coverage
func TestProviderRegionCoverage(t *testing.T) {
	tests := []struct {
		provider    string
		minRegions  int
		description string
	}{
		{ProviderAWSLightsail, 10, "AWS Lightsail"},
		{ProviderAlibabaSWAS, 10, "Alibaba SWAS International"},
		{ProviderTencentLighthouse, 5, "Tencent Lighthouse International"},
		{ProviderAliyunSWAS, 4, "Aliyun SWAS Domestic"},
		{ProviderQCloudLighthouse, 4, "QCloud Lighthouse Domestic"},
	}

	for _, tt := range tests {
		t.Run(tt.description, func(t *testing.T) {
			regions := ListRegionsForProvider(tt.provider)
			if len(regions) < tt.minRegions {
				t.Errorf("%s should have at least %d regions, got %d", tt.description, tt.minRegions, len(regions))
			}
		})
	}
}

// TestAlibabaUAESupport specifically tests Alibaba Cloud UAE region support
func TestAlibabaUAESupport(t *testing.T) {
	regions := ListRegionsForProvider(ProviderAlibabaSWAS)

	foundUAE := false
	for _, r := range regions {
		if r.Country == "AE" {
			foundUAE = true
			break
		}
	}

	if !foundUAE {
		t.Error("Alibaba SWAS must support UAE region")
	}
}

// TestRegionUniqueness verifies no duplicate slugs or provider IDs
func TestRegionUniqueness(t *testing.T) {
	// Check for duplicate slugs
	slugs := make(map[string]bool)
	for _, region := range AllRegions {
		if slugs[region.Slug] {
			t.Errorf("Duplicate slug found: %s", region.Slug)
		}
		slugs[region.Slug] = true
	}

	// Check for duplicate provider region IDs within same provider
	providerRegions := make(map[string]map[string]string) // provider -> regionID -> slug
	for _, region := range AllRegions {
		for provider, providerID := range region.Providers {
			if providerRegions[provider] == nil {
				providerRegions[provider] = make(map[string]string)
			}
			if existingSlug, exists := providerRegions[provider][providerID]; exists {
				t.Errorf("Duplicate provider region ID: provider=%s, regionID=%s, slugs=%s and %s",
					provider, providerID, existingSlug, region.Slug)
			}
			providerRegions[provider][providerID] = region.Slug
		}
	}
}

// TestContinentCoverage verifies we have regions in all major continents
func TestContinentCoverage(t *testing.T) {
	continents := map[string][]string{
		"North America": {"US", "CA"},
		"Europe":        {"IE", "GB", "FR", "DE", "SE"},
		"Asia Pacific":  {"JP", "KR", "SG", "AU", "ID", "IN", "TH", "MY", "PH"},
		"Middle East":   {"AE", "SA"},
		"South America": {"BR"},
		"Greater China": {"HK", "TW", "CN"},
	}

	regionCountries := make(map[string]bool)
	for _, region := range AllRegions {
		regionCountries[region.Country] = true
	}

	for continent, countries := range continents {
		found := false
		for _, country := range countries {
			if regionCountries[country] {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("No regions found in %s", continent)
		}
	}
}
