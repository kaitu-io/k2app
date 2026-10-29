//go:build darwin

package appext

// nePlatform is the Darwin NE platform networking component.
// It provides interface-bound dialing, network change detection,
// and TUN self-exclusion — all sharing one DefaultInterfaceMonitor.
//
// Mirrors daemon's architecture:
//
//	daemon: directDialer + singTunMonitor + ifaceMon (3 separate types, shared monitor)
//	appext: nePlatform (1 type, 3 roles, same shared monitor)
//
// Implements:
//   - wire.DirectDialer (DialContext, ListenPacket)
//   - wire.DialerController (ControlFunc)
//   - engine.NetworkChangeNotifier (Start, Close)

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"runtime/debug"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	tun "github.com/sagernet/sing-tun"
	"github.com/sagernet/sing/common/control"
)

// bindReemitInterval bounds how often a still-pathological direct-bind (utun
// or nil) is re-logged, so the decisive line survives 20MB log rotation during
// a multi-minute dial storm without flooding the log itself.
const bindReemitInterval = 30 * time.Second

// bindLogState records the last logged direct-bind resolution so the
// per-socket controlFunc only emits a DIAG line when the resolved interface
// identity changes — never per call. Identity is compared by name/index/isTun,
// deliberately ignoring the network family so a stable interface does not
// flap-log between tcp4/udp6 sockets; `at` drives the pathological re-emit.
type bindLogState struct {
	name  string
	index int
	isTun bool
	at    time.Time
}

type nePlatform struct {
	finder   control.InterfaceFinder
	netMon   tun.NetworkUpdateMonitor
	ifaceMon tun.DefaultInterfaceMonitor
	closed   sync.Once
	notify   func()                       // engine network-change callback, invoked by onInterfaceChange
	lastBind atomic.Pointer[bindLogState] // gates DIAG: direct-bind, see logBindDecision
}

// isTunName reports whether an interface name is a utun (the NE's own TUN).
// Used to detect the self-binding loop where the default-interface probe
// resolves to the tunnel instead of the physical uplink.
func isTunName(name string) bool { return strings.HasPrefix(name, "utun") }

// newNEPlatform creates the Darwin NE platform networking component.
// Uses UnderNetworkExtension: true so sing-tun detects the default interface
// via socket probe (getDefaultInterfaceBySocket) instead of route table parsing,
// which reflects VPN routes inside the NE process.
func newNEPlatform() (*nePlatform, error) {
	netMon, err := tun.NewNetworkUpdateMonitor(&neSlogAdapter{})
	if err != nil {
		return nil, fmt.Errorf("NE network monitor: %w", err)
	}
	finder := control.NewDefaultInterfaceFinder()
	ifaceMon, err := tun.NewDefaultInterfaceMonitor(netMon, &neSlogAdapter{}, tun.DefaultInterfaceMonitorOptions{
		InterfaceFinder:       finder,
		UnderNetworkExtension: true,
	})
	if err != nil {
		netMon.Close()
		return nil, fmt.Errorf("NE interface monitor: %w", err)
	}
	return &nePlatform{
		finder:   finder,
		netMon:   netMon,
		ifaceMon: ifaceMon,
	}, nil
}

// --- wire.DirectDialer ---

func (p *nePlatform) DialContext(ctx context.Context, network, addr string) (net.Conn, error) {
	nd := &net.Dialer{Control: p.controlFunc()}
	return nd.DialContext(ctx, network, addr)
}

func (p *nePlatform) ListenPacket(network, addr string) (net.PacketConn, error) {
	lc := &net.ListenConfig{Control: p.controlFunc()}
	return lc.ListenPacket(context.Background(), network, addr)
}

// --- wire.DialerController ---

func (p *nePlatform) ControlFunc() func(network, address string, c syscall.RawConn) error {
	return p.controlFunc()
}

// --- engine.NetworkChangeNotifier ---

func (p *nePlatform) Start(callback func()) error {
	p.notify = callback
	p.ifaceMon.RegisterCallback(p.onInterfaceChange)
	if err := p.netMon.Start(); err != nil {
		return err
	}
	return p.ifaceMon.Start()
}

// onInterfaceChange is the sing-tun DefaultInterfaceMonitor callback. The
// monitor invokes it from an unrecovered timer goroutine
// (time.AfterFunc → postCheckUpdate → emit — none of sing-tun's emit path has a
// recover), so a panic here crashes the whole Network Extension process and
// drops the user's tunnel. The deferred recover converts that into one dropped
// change-notification plus a loud DIAG line — the correct trade for a
// fire-and-forget network-change signal, where surviving always beats crashing.
//
// The known historical crash was a nil `defaultInterface` deref (k2 ≤0.4.7,
// sing-tun emits nil on the no-route path); the nil branch below fixes that
// specific bug, and the recover is the structural backstop for the next unknown
// panic in this callback (including anything the engine `notify` does).
func (p *nePlatform) onInterfaceChange(defaultInterface *control.Interface, flags int) {
	defer func() {
		if r := recover(); r != nil {
			slog.Warn("DIAG: ne-callback-panic",
				"panic", fmt.Sprint(r),
				"stack", string(debug.Stack()))
		}
	}()
	// DIAG Event A: record what the default-interface probe resolved to on
	// every change. When this is a utun*, direct sockets get bound to the TUN
	// and loop back into gVisor (the "国内直连全挂" failure). Emitted at INFO so
	// it survives the default build log level.
	if defaultInterface != nil {
		slog.Info("DIAG: ne-iface-resolved",
			"name", defaultInterface.Name,
			"index", defaultInterface.Index,
			"isTun", isTunName(defaultInterface.Name),
			"flags", flags)
	} else {
		slog.Warn("DIAG: ne-iface-resolved", "result", "nil", "flags", flags)
	}
	if p.notify != nil {
		p.notify()
	}
}

// Close stops monitors. Idempotent — engine.Stop() calls this via NetworkMonitor.Close().
func (p *nePlatform) Close() error {
	p.closed.Do(func() {
		p.ifaceMon.Close()
		p.netMon.Close()
	})
	return nil
}

// --- Accessor ---

// InterfaceMonitor returns the shared DefaultInterfaceMonitor for engine.Config.InterfaceMonitor.
// The TUN provider uses this for self-exclusion via RegisterMyInterface.
func (p *nePlatform) InterfaceMonitor() tun.DefaultInterfaceMonitor {
	return p.ifaceMon
}

// interfaceName returns the current default interface name (for logging).
func (p *nePlatform) interfaceName() string {
	if iface := p.ifaceMon.DefaultInterface(); iface != nil {
		return iface.Name
	}
	return ""
}

// --- internal ---

func (p *nePlatform) controlFunc() control.Func {
	// Wrap BindToInterface0 directly instead of using BindToInterfaceFunc.
	// When the monitor hasn't resolved the default interface yet (nil during
	// the ~20ms startup window), return nil to skip binding — NECP handles
	// NE socket bypass regardless. BindToInterfaceFunc with ("", -1) would
	// error with "interface not found".
	return func(network, address string, conn syscall.RawConn) error {
		iface := p.ifaceMon.DefaultInterface()
		if iface == nil {
			p.logBindDecision("", -1, false, network)
			return nil // no interface yet — NECP auto-bypasses NE sockets
		}
		p.logBindDecision(iface.Name, iface.Index, isTunName(iface.Name), network)
		return control.BindToInterface0(p.finder, conn, network, address, iface.Name, iface.Index, false)
	}
}

// logBindDecision emits DIAG Event B — the interface a direct socket is
// actually bound to — but only when the resolved identity changes, so a 512/s
// dial storm produces one line, not 512. While the resolution is pathological
// (a utun bind, or nil), it is re-emitted at most once per bindReemitInterval
// so the decisive line survives 20MB log rotation across a multi-minute storm.
// Loads before allocating so the suppressed common path stays alloc-free on
// this per-socket hot path. A utun bind or nil resolution is logged at WARN;
// a physical uplink at INFO. name=="" means DefaultInterface() was nil.
func (p *nePlatform) logBindDecision(name string, index int, isTun bool, network string) {
	prev := p.lastBind.Load()
	changed := prev == nil || prev.name != name || prev.index != index || prev.isTun != isTun
	pathological := isTun || name == ""
	stale := !changed && pathological && time.Since(prev.at) >= bindReemitInterval
	if !changed && !stale {
		return // unchanged, and either healthy or logged recently
	}
	p.lastBind.Store(&bindLogState{name: name, index: index, isTun: isTun, at: time.Now()})
	if name == "" {
		slog.Warn("DIAG: direct-bind", "resolved", "nil", "net", network)
		return
	}
	if isTun {
		slog.Warn("DIAG: direct-bind",
			"resolved", name, "index", index, "isTun", true, "net", network)
		return
	}
	slog.Info("DIAG: direct-bind",
		"resolved", name, "index", index, "isTun", false, "net", network)
}

// neSlogAdapter bridges sing's logger to Go's slog.
type neSlogAdapter struct{}

func (l *neSlogAdapter) Trace(args ...any) { slog.Debug("[sing-tun] " + fmt.Sprint(args...)) }
func (l *neSlogAdapter) Debug(args ...any) { slog.Debug("[sing-tun] " + fmt.Sprint(args...)) }
func (l *neSlogAdapter) Info(args ...any)  { slog.Info("[sing-tun] " + fmt.Sprint(args...)) }
func (l *neSlogAdapter) Warn(args ...any)  { slog.Warn("[sing-tun] " + fmt.Sprint(args...)) }
func (l *neSlogAdapter) Error(args ...any) { slog.Error("[sing-tun] " + fmt.Sprint(args...)) }
func (l *neSlogAdapter) Fatal(args ...any) { slog.Error("[sing-tun] FATAL: " + fmt.Sprint(args...)) }
func (l *neSlogAdapter) Panic(args ...any) { slog.Error("[sing-tun] PANIC: " + fmt.Sprint(args...)) }
