package probe

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/kaitu-io/k2/safego"
	"github.com/kaitu-io/k2/wire"
)

// URLSource yields the URLs to probe each cycle. Daemon supplies active
// subscription tunnels; tests supply a static list.
type URLSource interface {
	CandidateURLs() []string
}

// Prober is the probe function injected into Service. Production:
// wire.ProbeURL. Tests: stub.
type Prober func(ctx context.Context, rawURL string, dd wire.DirectDialer, rounds int, interval time.Duration) (wire.ProbeStats, error)

type Config struct {
	Registry      *Registry
	Source        URLSource
	DirectDialer  wire.DirectDialer
	Cadence       time.Duration // default 5m
	MaxConcurrent int           // default 4
	Rounds        int           // default 8
	RoundInterval time.Duration // default 50ms
	Prober        Prober        // default wire.ProbeURL
}

type Service struct {
	cfg  Config
	once sync.Once
	stop context.CancelFunc
	done chan struct{}
}

func NewService(cfg Config) *Service {
	if cfg.Cadence <= 0 {
		cfg.Cadence = 5 * time.Minute
	}
	if cfg.MaxConcurrent <= 0 {
		cfg.MaxConcurrent = 4
	}
	if cfg.Rounds <= 0 {
		cfg.Rounds = 8
	}
	if cfg.RoundInterval <= 0 {
		cfg.RoundInterval = 50 * time.Millisecond
	}
	if cfg.Prober == nil {
		cfg.Prober = wire.ProbeURL
	}
	return &Service{cfg: cfg, done: make(chan struct{})}
}

func (s *Service) Start(parent context.Context) {
	s.once.Do(func() {
		ctx, cancel := context.WithCancel(parent)
		s.stop = cancel
		safego.Go(func() {
			defer close(s.done)
			s.loop(ctx)
		})
	})
}

func (s *Service) Close() {
	if s.stop == nil {
		return // never started — nothing to cancel or wait on
	}
	s.stop()
	select {
	case <-s.done:
	case <-time.After(5 * time.Second):
		slog.Warn("probe.Service: Close wait timed out")
	}
}

func (s *Service) loop(ctx context.Context) {
	s.cycle(ctx)
	ticker := time.NewTicker(s.cfg.Cadence)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.cycle(ctx)
		}
	}
}

func (s *Service) cycle(ctx context.Context) {
	urls := s.cfg.Source.CandidateURLs()
	if len(urls) == 0 {
		return
	}
	sem := make(chan struct{}, s.cfg.MaxConcurrent)
	var wg sync.WaitGroup
	for _, u := range urls {
		select {
		case <-ctx.Done():
			wg.Wait()
			return
		case sem <- struct{}{}:
		}
		wg.Add(1)
		url := u
		safego.Go(func() {
			defer wg.Done()
			defer func() { <-sem }()
			probeCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
			defer cancel()
			stats, err := s.cfg.Prober(probeCtx, url, s.cfg.DirectDialer, s.cfg.Rounds, s.cfg.RoundInterval)
			if err != nil {
				stats.MeasuredAt = time.Now()
			}
			s.cfg.Registry.Record(url, stats)
		})
	}
	wg.Wait()
}
