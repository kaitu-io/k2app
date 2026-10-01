package center

import "time"

const (
	funnelMaxFuture = 5 * time.Minute
	funnelMaxPast   = 7 * 24 * time.Hour
)

// clampOccurredAt 信任客户端时间，但限在 [now-7d, now+5min]；越界则用服务端 now。
func clampOccurredAt(client, now time.Time) time.Time {
	if client.After(now.Add(funnelMaxFuture)) || client.Before(now.Add(-funnelMaxPast)) {
		return now
	}
	return client
}
