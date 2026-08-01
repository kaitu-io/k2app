package subscription

import (
	"context"
	"crypto/tls"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/kaitu-io/k2/config"
)

// Resolve scans cfg.Routes for k2subs:// URLs, fetches each subscription,
// and picks an initial server. Returns the new sessions (also stored on m)
// and a map of route index → resolved k2v5:// URL. Caller passes the
// resolved map to ApplyResolved for the engine config.
//
// On error, all already-created (new) sessions are closed before returning.
//
// Replace semantics: Resolve closes any previously-resolved sessions on
// entry. This makes the call idempotent for callers like gateway whose
// closeTunnel path does not itself call CloseAll — back-to-back doUp
// requests cannot orphan a prior session's refresh loop or HTTP transport.
// Callers still call CloseAll on doDown to release after the engine stops.
func (m *Manager) Resolve(cfg *config.ClientConfig) (sessions []*Session, resolved map[int]string, err error) {
	// Drain any sessions left over from a prior Resolve. CloseAll is
	// idempotent — safe even when the caller already called it.
	m.CloseAll()

	resolved = make(map[int]string)
	closeAll := func(ss []*Session) {
		for _, s := range ss {
			s.Close()
		}
	}

	for i, route := range cfg.Routes {
		if !strings.HasPrefix(route.Via, "k2subs://") {
			continue
		}

		sub, parseErr := config.NewSubscription(route.Via, m.cacheDir)
		if parseErr != nil {
			slog.Error("subscription: parse failed",
				"via", config.TruncateVia(route.Via),
				"err", parseErr)
			closeAll(sessions)
			return nil, nil, fmt.Errorf("subscription parse: %w", parseErr)
		}

		if m.scoreSrc != nil {
			sub.SetScoreSource(m.scoreSrc)
		}

		sess := &Session{sub: sub}
		if hc, ok := m.httpClient.(*http.Client); ok && hc != nil {
			sess.httpClient = hc
		} else {
			sess.httpTransport = &http.Transport{
				TLSClientConfig: &tls.Config{MinVersion: tls.VersionTLS12},
			}
			sess.httpClient = &http.Client{
				Timeout:   15 * time.Second,
				Transport: sess.httpTransport,
			}
		}

		fetchCtx, fetchCancel := context.WithTimeout(context.Background(), 15*time.Second)
		fetchErr := sub.Fetch(fetchCtx, sess.httpClient)
		fetchCancel()

		if fetchErr != nil {
			if len(sub.Tunnels()) == 0 {
				slog.Error("subscription: fetch failed with no cached data",
					"via", config.TruncateVia(route.Via),
					"err", fetchErr)
				sess.Close()
				closeAll(sessions)
				return nil, nil, fmt.Errorf("subscription fetch failed (no cache): %w", fetchErr)
			}
			slog.Warn("subscription: fetch failed, using cached data", "err", fetchErr)
			// spec §4.4 P5：降级本身是正确的可用性设计（保留），但必须报告
			// "正在用多久以前的数据"。缓存年龄 = 缓存文件 mtime 距今。
			if age, ok := sub.CacheAge(); ok {
				slog.Warn("DIAG: subs-stale-cache",
					"via", config.TruncateVia(route.Via),
					"cacheAgeS", int64(age.Seconds()),
					"err", fetchErr)
			}
		}

		tunnel, pickErr := sub.Pick(nil)
		if pickErr != nil {
			slog.Error("subscription: pick failed",
				"via", config.TruncateVia(route.Via),
				"servers", len(sub.Tunnels()),
				"err", pickErr)
			sess.Close()
			closeAll(sessions)
			return nil, nil, fmt.Errorf("subscription pick: %w", pickErr)
		}

		slog.Info("subscription: resolved",
			"to", config.TruncateVia(tunnel.URL),
			"servers", len(sub.Tunnels()),
		)

		resolved[i] = tunnel.URL
		sessions = append(sessions, sess)
	}

	m.mu.Lock()
	m.sessions = sessions
	m.mu.Unlock()

	return sessions, resolved, nil
}

// CloseAll closes every active Session and clears the slice. Idempotent.
func (m *Manager) CloseAll() {
	m.mu.Lock()
	sessions := m.sessions
	m.sessions = nil
	m.mu.Unlock()

	for _, s := range sessions {
		s.Close()
	}
}

// ApplyResolved returns a shallow copy of cfg with k2subs:// routes replaced
// by their resolved k2v5:// URLs. Original cfg is not modified — callers
// keep the k2subs:// version for persistence so a restart re-fetches.
func ApplyResolved(cfg *config.ClientConfig, resolved map[int]string) *config.ClientConfig {
	if len(resolved) == 0 {
		return cfg
	}
	engineCfg := *cfg
	engineCfg.Routes = make([]config.RouteConfig, len(cfg.Routes))
	copy(engineCfg.Routes, cfg.Routes)
	for i, url := range resolved {
		engineCfg.Routes[i].Via = url
	}
	engineCfg.Server = ""
	config.SetDefaults(&engineCfg)
	return &engineCfg
}
