package center

import "time"

const funnelMaxPast = 7 * 24 * time.Hour

// clampOccurredAt 信任客户端时间，但限在 [now-7d, now]：晚于服务端接收时刻（时钟快了，
// 没有容差——事件不可能发生在收到它之后）或早于 7 天前，一律用服务端接收时刻。
func clampOccurredAt(client, now time.Time) time.Time {
	if client.After(now) || client.Before(now.Add(-funnelMaxPast)) {
		return now
	}
	return client
}
