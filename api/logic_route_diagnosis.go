package center

import (
	"context"
	"crypto/hmac"
	"crypto/sha1"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/wordgate/qtoolkit/log"
)

// Alibaba Cloud region and carrier definitions for traceroute probes
var (
	// Chinese provinces with carrier coverage
	// Format: "city_isp" where isp is: telecom, unicom, mobile
	DiagnosisProbes = []string{
		// Tier 1 cities
		"beijing_telecom", "beijing_unicom", "beijing_mobile",
		"shanghai_telecom", "shanghai_unicom", "shanghai_mobile",
		"guangzhou_telecom", "guangzhou_unicom", "guangzhou_mobile",
		"shenzhen_telecom", "shenzhen_unicom", "shenzhen_mobile",
		// Tier 2 cities
		"hangzhou_telecom", "hangzhou_unicom", "hangzhou_mobile",
		"chengdu_telecom", "chengdu_unicom", "chengdu_mobile",
		"nanjing_telecom", "nanjing_unicom", "nanjing_mobile",
		"wuhan_telecom", "wuhan_unicom", "wuhan_mobile",
		"xian_telecom", "xian_unicom", "xian_mobile",
		// Northern regions
		"shenyang_telecom", "shenyang_unicom", "shenyang_mobile",
		"jinan_telecom", "jinan_unicom", "jinan_mobile",
		"zhengzhou_telecom", "zhengzhou_unicom", "zhengzhou_mobile",
		// Southern regions
		"fuzhou_telecom", "fuzhou_unicom", "fuzhou_mobile",
		"changsha_telecom", "changsha_unicom", "changsha_mobile",
	}

	// Map city to province
	CityToProvince = map[string]string{
		"beijing":   "beijing",
		"shanghai":  "shanghai",
		"guangzhou": "guangdong",
		"shenzhen":  "guangdong",
		"hangzhou":  "zhejiang",
		"chengdu":   "sichuan",
		"nanjing":   "jiangsu",
		"wuhan":     "hubei",
		"xian":      "shaanxi",
		"shenyang":  "liaoning",
		"jinan":     "shandong",
		"zhengzhou": "henan",
		"fuzhou":    "fujian",
		"changsha":  "hunan",
	}

	// Map ISP names
	ISPNames = map[string]string{
		"telecom": "china_telecom",
		"unicom":  "china_unicom",
		"mobile":  "china_mobile",
	}
)

// RouteType represents the detected route type
type RouteType string

const (
	RouteTypeCN2GIA   RouteType = "cn2_gia"   // China Telecom CN2 GIA (premium)
	RouteTypeCN2GT    RouteType = "cn2_gt"    // China Telecom CN2 GT
	RouteType163      RouteType = "163"       // China Telecom 163 (regular)
	RouteTypeCMI      RouteType = "cmi"       // China Mobile CMI
	RouteTypeAS9929   RouteType = "as9929"    // China Unicom AS9929 (premium)
	RouteTypeAS4837   RouteType = "as4837"    // China Unicom AS4837 (regular)
	RouteTypeUnknown  RouteType = "unknown"   // Unknown route type
)

// TracerouteResult represents a single traceroute result
type TracerouteResult struct {
	Probe     string    `json:"probe"`     // e.g., "guangzhou_telecom"
	Province  string    `json:"province"`  // e.g., "guangdong"
	Carrier   string    `json:"carrier"`   // e.g., "china_telecom"
	Target    string    `json:"target"`    // Target IP
	Hops      []HopInfo `json:"hops"`      // Traceroute hops
	RouteType RouteType `json:"routeType"` // Detected route type
	AvgRTT    float64   `json:"avgRtt"`    // Average RTT in ms
	Timestamp time.Time `json:"timestamp"`
}

// HopInfo represents a single hop in traceroute
type HopInfo struct {
	Hop     int     `json:"hop"`
	IP      string  `json:"ip"`
	RTT     float64 `json:"rtt"` // in ms
	ASN     string  `json:"asn,omitempty"`
	ISP     string  `json:"isp,omitempty"`
	Country string  `json:"country,omitempty"`
	Region  string  `json:"region,omitempty"`
}

// AliyunTracerouteRequest represents Alibaba Cloud instant detection request
type AliyunTracerouteRequest struct {
	AccessKeyID     string
	AccessKeySecret string
	Address         string   // Target IP/domain
	TaskType        string   // MTR or Traceroute
	IspCities       []string // List of "city_isp" probe locations
}

// AliyunTracerouteResponse represents the API response
type AliyunTracerouteResponse struct {
	RequestID string `json:"RequestId"`
	Code      string `json:"Code"`
	Message   string `json:"Message"`
	Success   bool   `json:"Success"`
	Data      struct {
		TaskID string `json:"TaskId"`
	} `json:"Data"`
}

// AliyunTaskResultResponse represents task result response
type AliyunTaskResultResponse struct {
	RequestID string `json:"RequestId"`
	Code      string `json:"Code"`
	Message   string `json:"Message"`
	Success   bool   `json:"Success"`
	Data      struct {
		Status  string                `json:"Status"` // RUNNING, FINISHED
		Results []AliyunProbeResult   `json:"Results"`
	} `json:"Data"`
}

// AliyunProbeResult represents a single probe's result
type AliyunProbeResult struct {
	City      string  `json:"City"`
	Isp       string  `json:"Isp"`
	AvgRtt    float64 `json:"AvgRtt"`
	LossRate  float64 `json:"LossRate"`
	TraceData string  `json:"TraceData"` // JSON string of trace hops
}

// RunNodeDiagnosis runs traceroute diagnosis for a node from all configured probes
func RunNodeDiagnosis(ctx context.Context, nodeIP string, accessKeyID, accessKeySecret string) ([]TracerouteResult, error) {
	log.Infof(ctx, "[DIAGNOSIS] Starting diagnosis for node %s", nodeIP)

	// Create instant detection task
	taskID, err := createAliyunTracerouteTask(ctx, accessKeyID, accessKeySecret, nodeIP, DiagnosisProbes)
	if err != nil {
		return nil, fmt.Errorf("failed to create traceroute task: %w", err)
	}

	log.Infof(ctx, "[DIAGNOSIS] Task created: %s, waiting for results...", taskID)

	// Poll for results (max 60 seconds)
	var results []TracerouteResult
	for i := 0; i < 12; i++ {
		time.Sleep(5 * time.Second)

		taskResult, err := getAliyunTaskResult(ctx, accessKeyID, accessKeySecret, taskID)
		if err != nil {
			log.Warnf(ctx, "[DIAGNOSIS] Failed to get task result: %v", err)
			continue
		}

		if taskResult.Data.Status == "FINISHED" {
			results = parseAliyunResults(taskResult.Data.Results, nodeIP)
			break
		}
	}

	if len(results) == 0 {
		return nil, fmt.Errorf("no results received for task %s", taskID)
	}

	log.Infof(ctx, "[DIAGNOSIS] Completed: %d probe results for node %s", len(results), nodeIP)
	return results, nil
}

// createAliyunTracerouteTask creates an instant traceroute task
func createAliyunTracerouteTask(ctx context.Context, accessKeyID, accessKeySecret, target string, probes []string) (string, error) {
	// Build IspCities parameter
	ispCities := make([]map[string]string, 0, len(probes))
	for _, probe := range probes {
		parts := strings.Split(probe, "_")
		if len(parts) != 2 {
			continue
		}
		ispCities = append(ispCities, map[string]string{
			"City": parts[0],
			"Isp":  parts[1],
		})
	}
	ispCitiesJSON, _ := json.Marshal(ispCities)

	// Build request parameters
	params := map[string]string{
		"Action":           "CreateInstantSiteMonitor",
		"Format":           "JSON",
		"Version":          "2019-01-01",
		"SignatureMethod":  "HMAC-SHA1",
		"SignatureVersion": "1.0",
		"SignatureNonce":   uuid.New().String(),
		"Timestamp":        time.Now().UTC().Format("2006-01-02T15:04:05Z"),
		"AccessKeyId":      accessKeyID,
		"TaskType":         "MTR",
		"Address":          target,
		"IspCities":        string(ispCitiesJSON),
	}

	// Sign request
	signature := signAliyunRequest(params, accessKeySecret, "POST")
	params["Signature"] = signature

	// Build form data
	form := url.Values{}
	for k, v := range params {
		form.Set(k, v)
	}

	// Make request
	req, err := http.NewRequestWithContext(ctx, "POST", "https://metrics.aliyuncs.com/", strings.NewReader(form.Encode()))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)

	var result AliyunTracerouteResponse
	if err := json.Unmarshal(body, &result); err != nil {
		return "", fmt.Errorf("failed to parse response: %w", err)
	}

	if !result.Success {
		return "", fmt.Errorf("API error: %s - %s", result.Code, result.Message)
	}

	return result.Data.TaskID, nil
}

// getAliyunTaskResult retrieves task results
func getAliyunTaskResult(ctx context.Context, accessKeyID, accessKeySecret, taskID string) (*AliyunTaskResultResponse, error) {
	params := map[string]string{
		"Action":           "DescribeInstantSiteMonitorLog",
		"Format":           "JSON",
		"Version":          "2019-01-01",
		"SignatureMethod":  "HMAC-SHA1",
		"SignatureVersion": "1.0",
		"SignatureNonce":   uuid.New().String(),
		"Timestamp":        time.Now().UTC().Format("2006-01-02T15:04:05Z"),
		"AccessKeyId":      accessKeyID,
		"TaskId":           taskID,
	}

	signature := signAliyunRequest(params, accessKeySecret, "GET")
	params["Signature"] = signature

	// Build query string
	query := url.Values{}
	for k, v := range params {
		query.Set(k, v)
	}

	reqURL := "https://metrics.aliyuncs.com/?" + query.Encode()
	req, err := http.NewRequestWithContext(ctx, "GET", reqURL, nil)
	if err != nil {
		return nil, err
	}

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)

	var result AliyunTaskResultResponse
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}

	return &result, nil
}

// signAliyunRequest signs the request using Alibaba Cloud signature method
func signAliyunRequest(params map[string]string, accessKeySecret, httpMethod string) string {
	// Sort keys
	keys := make([]string, 0, len(params))
	for k := range params {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	// Build canonical query string
	var pairs []string
	for _, k := range keys {
		pairs = append(pairs, fmt.Sprintf("%s=%s",
			url.QueryEscape(k),
			url.QueryEscape(params[k])))
	}
	canonicalQuery := strings.Join(pairs, "&")

	// Build string to sign
	stringToSign := fmt.Sprintf("%s&%s&%s",
		httpMethod,
		url.QueryEscape("/"),
		url.QueryEscape(canonicalQuery))

	// Calculate HMAC-SHA1
	h := hmac.New(sha1.New, []byte(accessKeySecret+"&"))
	h.Write([]byte(stringToSign))
	signature := base64.StdEncoding.EncodeToString(h.Sum(nil))

	return signature
}

// parseAliyunResults converts Alibaba Cloud results to our format
func parseAliyunResults(probeResults []AliyunProbeResult, target string) []TracerouteResult {
	var results []TracerouteResult

	for _, pr := range probeResults {
		probe := fmt.Sprintf("%s_%s", strings.ToLower(pr.City), strings.ToLower(pr.Isp))
		province := CityToProvince[strings.ToLower(pr.City)]
		carrier := ISPNames[strings.ToLower(pr.Isp)]

		// Parse trace data
		var hops []HopInfo
		if pr.TraceData != "" {
			json.Unmarshal([]byte(pr.TraceData), &hops)
		}

		// Detect route type from hops
		routeType := detectRouteType(hops, carrier)

		results = append(results, TracerouteResult{
			Probe:     probe,
			Province:  province,
			Carrier:   carrier,
			Target:    target,
			Hops:      hops,
			RouteType: routeType,
			AvgRTT:    pr.AvgRtt,
			Timestamp: time.Now(),
		})
	}

	return results
}

// detectRouteType analyzes hops to determine route type
func detectRouteType(hops []HopInfo, carrier string) RouteType {
	for _, hop := range hops {
		asn := strings.ToUpper(hop.ASN)
		isp := strings.ToLower(hop.ISP)

		// CN2 detection (AS4809)
		if strings.Contains(asn, "4809") || strings.Contains(isp, "cn2") {
			if strings.Contains(isp, "gia") {
				return RouteTypeCN2GIA
			}
			return RouteTypeCN2GT
		}

		// CMI detection (AS58453)
		if strings.Contains(asn, "58453") || strings.Contains(isp, "cmi") {
			return RouteTypeCMI
		}

		// AS9929 detection (China Unicom premium)
		if strings.Contains(asn, "9929") {
			return RouteTypeAS9929
		}

		// AS4837 detection (China Unicom regular)
		if strings.Contains(asn, "4837") {
			return RouteTypeAS4837
		}

		// 163 backbone detection (AS4134)
		if strings.Contains(asn, "4134") {
			return RouteType163
		}
	}

	return RouteTypeUnknown
}

// AggregateRouteInfo aggregates diagnosis results into a summary for storage
func AggregateRouteInfo(results []TracerouteResult) map[string]RouteType {
	// Key: "carrier:province", Value: most common route type
	routeMap := make(map[string]RouteType)

	for _, r := range results {
		key := fmt.Sprintf("%s:%s", r.Carrier, r.Province)
		routeMap[key] = r.RouteType
	}

	return routeMap
}
