package center

import (
	db "github.com/wordgate/qtoolkit/db"
	"context"
	"crypto/tls"
	"fmt"
	"io"
	"net"
	"net/http"
	"sync"
	"time"

	"github.com/wordgate/qtoolkit/log"
)

const (
	// 健康检查超时时间
	healthCheckTimeout = 10 * time.Second
	// 并发检查的最大数量
	maxConcurrentChecks = 10
)

// TunnelHealthResult 隧道健康检查结果
type TunnelHealthResult struct {
	TunnelID  uint64
	Domain    string
	IsHealthy bool
	ErrorMsg  string
	Latency   time.Duration // 响应延迟
}

// CheckTunnelHealth 检查单个隧道的健康状态
// 通过连接隧道并请求 /version 来判断服务是否正常
func CheckTunnelHealth(ctx context.Context, tunnel *SlaveTunnel) TunnelHealthResult {
	result := TunnelHealthResult{
		TunnelID:  tunnel.ID,
		Domain:    tunnel.Domain,
		IsHealthy: false,
	}

	// Only check k2 protocol
	if tunnel.Protocol != TunnelProtocolK2 {
		result.ErrorMsg = fmt.Sprintf("skipped: protocol %s not supported", tunnel.Protocol)
		return result
	}

	// 获取关联的节点信息
	if tunnel.Node == nil {
		result.ErrorMsg = "node information not available"
		return result
	}

	startTime := time.Now()

	// 创建带超时的上下文
	checkCtx, cancel := context.WithTimeout(ctx, healthCheckTimeout)
	defer cancel()

	// 构建健康检查 URL
	// 使用 https 协议和 /version 端点
	healthCheckURL := fmt.Sprintf("https://%s/version", tunnel.Domain)

	// 创建 HTTP 客户端，配置 TLS 和超时
	client := &http.Client{
		Timeout: healthCheckTimeout,
		Transport: &http.Transport{
			// 自定义 Dialer，强制使用节点的 IP 地址
			DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
				// 使用节点的 IPv4 地址
				targetAddr := net.JoinHostPort(tunnel.Node.Ipv4, fmt.Sprintf("%d", tunnel.Port))
				log.Debugf(ctx, "[health-check] Dialing %s for domain %s", targetAddr, tunnel.Domain)
				return (&net.Dialer{
					Timeout:   healthCheckTimeout,
					KeepAlive: 0,
				}).DialContext(ctx, "tcp", targetAddr)
			},
			TLSClientConfig: &tls.Config{
				// 使用 tunnel 的 domain 作为 SNI
				ServerName: tunnel.Domain,
				// 在生产环境中，应该配置适当的证书验证
				// 这里为了兼容性，暂时跳过验证
				InsecureSkipVerify: true,
			},
			// 禁用 keep-alive，每次都建立新连接
			DisableKeepAlives: true,
		},
	}

	// 创建 HTTP 请求
	req, err := http.NewRequestWithContext(checkCtx, "GET", healthCheckURL, nil)
	if err != nil {
		result.ErrorMsg = fmt.Sprintf("failed to create request: %v", err)
		log.Errorf(ctx, "[health-check] Tunnel %s (%d): %s", tunnel.Domain, tunnel.ID, result.ErrorMsg)
		return result
	}

	// 发送请求
	log.Debugf(ctx, "[health-check] Checking tunnel %s (%d) at %s via %s:%d",
		tunnel.Domain, tunnel.ID, healthCheckURL, tunnel.Node.Ipv4, tunnel.Port)

	resp, err := client.Do(req)
	if err != nil {
		result.ErrorMsg = fmt.Sprintf("request failed: %v", err)
		log.Warnf(ctx, "[health-check] Tunnel %s (%d): %s", tunnel.Domain, tunnel.ID, result.ErrorMsg)
		return result
	}
	defer resp.Body.Close()

	// 记录延迟
	result.Latency = time.Since(startTime)

	// 读取响应体（限制大小避免占用过多内存）
	body, err := io.ReadAll(io.LimitReader(resp.Body, 4096))
	if err != nil {
		result.ErrorMsg = fmt.Sprintf("failed to read response: %v", err)
		log.Warnf(ctx, "[health-check] Tunnel %s (%d): %s", tunnel.Domain, tunnel.ID, result.ErrorMsg)
		return result
	}

	// 检查 HTTP 状态码
	if resp.StatusCode != http.StatusOK {
		result.ErrorMsg = fmt.Sprintf("unexpected status code: %d, body: %s", resp.StatusCode, string(body))
		log.Warnf(ctx, "[health-check] Tunnel %s (%d): %s", tunnel.Domain, tunnel.ID, result.ErrorMsg)
		return result
	}

	// 健康检查成功
	result.IsHealthy = true
	log.Infof(ctx, "[health-check] Tunnel %s (%d) is healthy (latency: %v)", tunnel.Domain, tunnel.ID, result.Latency)

	return result
}

// CheckAllK2WSSTunnelsHealth 检查所有 k2wss 隧道的健康状态
func CheckAllK2WSSTunnelsHealth(ctx context.Context) ([]TunnelHealthResult, error) {
	log.Infof(ctx, "[health-check] Starting health check for all k2 tunnels")

	// Query all k2 protocol tunnels
	var tunnels []SlaveTunnel
	err := db.Get().
		Where(&SlaveTunnel{Protocol: TunnelProtocolK2}).
		Preload("Node"). // 预加载关联的节点信息
		Find(&tunnels).Error

	if err != nil {
		log.Errorf(ctx, "[health-check] Failed to query tunnels: %v", err)
		return nil, fmt.Errorf("failed to query tunnels: %w", err)
	}

	if len(tunnels) == 0 {
		log.Infof(ctx, "[health-check] No k2wss tunnels found")
		return []TunnelHealthResult{}, nil
	}

	log.Infof(ctx, "[health-check] Found %d k2wss tunnels to check", len(tunnels))

	// 使用并发检查，但限制并发数量
	results := make([]TunnelHealthResult, len(tunnels))
	semaphore := make(chan struct{}, maxConcurrentChecks)
	var wg sync.WaitGroup

	for i := range tunnels {
		wg.Add(1)
		go func(idx int, tunnel SlaveTunnel) {
			defer wg.Done()

			// 获取信号量
			semaphore <- struct{}{}
			defer func() { <-semaphore }()

			// 执行健康检查
			results[idx] = CheckTunnelHealth(ctx, &tunnel)
		}(i, tunnels[i])
	}

	wg.Wait()

	// 统计结果
	healthyCount := 0
	unhealthyCount := 0
	for _, result := range results {
		if result.IsHealthy {
			healthyCount++
		} else {
			unhealthyCount++
		}
	}

	log.Infof(ctx, "[health-check] Health check completed: %d healthy, %d unhealthy out of %d tunnels",
		healthyCount, unhealthyCount, len(tunnels))

	return results, nil
}

// UpdateTunnelHealthStatus 根据健康检查结果更新隧道的健康状态
// 目前将健康状态记录到日志中，后续可以扩展到数据库字段
func UpdateTunnelHealthStatus(ctx context.Context, results []TunnelHealthResult) error {
	for _, result := range results {
		if result.IsHealthy {
			log.Infof(ctx, "[health-check] Tunnel %s (%d): HEALTHY (latency: %v)",
				result.Domain, result.TunnelID, result.Latency)
		} else {
			log.Warnf(ctx, "[health-check] Tunnel %s (%d): UNHEALTHY - %s",
				result.Domain, result.TunnelID, result.ErrorMsg)
		}
	}

	// TODO: 将健康状态持久化到数据库
	// 可以在 SlaveTunnel 表中添加 IsHealthy 字段和 LastHealthCheckAt 字段

	return nil
}

// StartTunnelHealthCheckScheduler 启动隧道健康检查调度器
// 每小时执行一次健康检查
func StartTunnelHealthCheckScheduler(ctx context.Context) {
	log.Infof(ctx, "[health-check] Starting tunnel health check scheduler (interval: 1 hour)")

	// 立即执行一次健康检查
	go func() {
		if results, err := CheckAllK2WSSTunnelsHealth(ctx); err != nil {
			log.Errorf(ctx, "[health-check] Initial health check failed: %v", err)
		} else {
			UpdateTunnelHealthStatus(ctx, results)
		}
	}()

	// 启动定时器，每小时执行一次
	ticker := time.NewTicker(1 * time.Hour)
	go func() {
		for {
			select {
			case <-ticker.C:
				log.Infof(ctx, "[health-check] Running scheduled health check")
				if results, err := CheckAllK2WSSTunnelsHealth(ctx); err != nil {
					log.Errorf(ctx, "[health-check] Scheduled health check failed: %v", err)
				} else {
					UpdateTunnelHealthStatus(ctx, results)
				}
			case <-ctx.Done():
				ticker.Stop()
				log.Infof(ctx, "[health-check] Stopping tunnel health check scheduler")
				return
			}
		}
	}()
}
