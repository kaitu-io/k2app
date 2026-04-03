//go:build darwin && !ios

package provider

/*
#cgo LDFLAGS: -framework SystemConfiguration -framework CoreFoundation
#include <SystemConfiguration/SystemConfiguration.h>
#include <CoreFoundation/CoreFoundation.h>
#include <stdlib.h>
#include <string.h>
#include <stdio.h>

// Go callback forward declaration.
extern void goDNSChanged(void);

// Callback fired by configd when watched keys change.
static void dnsCallback(SCDynamicStoreRef store, CFArrayRef changedKeys, void *info) {
	goDNSChanged();
}

// Global state for the CFRunLoop monitor.
static SCDynamicStoreRef g_store = NULL;
static CFRunLoopSourceRef g_source = NULL;
static CFRunLoopRef g_runLoop = NULL;

// k2GetPrimaryServiceID returns the PrimaryService from State:/Network/Global/IPv4.
// Caller must free() the returned string. Returns NULL if unavailable.
static char* k2GetPrimaryServiceID() {
	SCDynamicStoreRef store = SCDynamicStoreCreate(NULL, CFSTR("k2-dns"), NULL, NULL);
	if (!store) return NULL;

	CFDictionaryRef dict = SCDynamicStoreCopyValue(store,
		CFSTR("State:/Network/Global/IPv4"));
	CFRelease(store);
	if (!dict) return NULL;

	CFStringRef svc = CFDictionaryGetValue(dict, CFSTR("PrimaryService"));
	if (!svc) {
		CFRelease(dict);
		return NULL;
	}

	char buf[128];
	if (!CFStringGetCString(svc, buf, sizeof(buf), kCFStringEncodingUTF8)) {
		CFRelease(dict);
		return NULL;
	}
	CFRelease(dict);
	return strdup(buf);
}

// Helper: build a DNS key for a given prefix and service ID.
// prefix is "State:" or "Setup:".
static CFStringRef buildDNSKey(const char* prefix, const char* serviceID) {
	char key[256];
	snprintf(key, sizeof(key), "%s/Network/Service/%s/DNS", prefix, serviceID);
	return CFStringCreateWithCString(NULL, key, kCFStringEncodingUTF8);
}

// k2GetStateDNS reads the first server from State:/Network/Service/<id>/DNS (DHCP-assigned).
// Caller must free() the returned string. Returns NULL if unavailable.
static char* k2GetStateDNS(const char* serviceID) {
	SCDynamicStoreRef store = SCDynamicStoreCreate(NULL, CFSTR("k2-dns"), NULL, NULL);
	if (!store) return NULL;

	CFStringRef cfKey = buildDNSKey("State:", serviceID);
	CFDictionaryRef dns = SCDynamicStoreCopyValue(store, cfKey);
	CFRelease(cfKey);
	CFRelease(store);
	if (!dns) return NULL;

	CFArrayRef servers = CFDictionaryGetValue(dns, CFSTR("ServerAddresses"));
	if (!servers || CFArrayGetCount(servers) == 0) {
		CFRelease(dns);
		return NULL;
	}

	CFStringRef first = CFArrayGetValueAtIndex(servers, 0);
	char buf[64];
	if (!CFStringGetCString(first, buf, sizeof(buf), kCFStringEncodingUTF8)) {
		CFRelease(dns);
		return NULL;
	}
	CFRelease(dns);
	return strdup(buf);
}

// k2GetSetupDNS reads the first server from Setup:/Network/Service/<id>/DNS.
// Caller must free() the returned string. Returns NULL if key doesn't exist.
static char* k2GetSetupDNS(const char* serviceID) {
	SCDynamicStoreRef store = SCDynamicStoreCreate(NULL, CFSTR("k2-dns"), NULL, NULL);
	if (!store) return NULL;

	CFStringRef cfKey = buildDNSKey("Setup:", serviceID);
	CFDictionaryRef dns = SCDynamicStoreCopyValue(store, cfKey);
	CFRelease(cfKey);
	CFRelease(store);
	if (!dns) return NULL;

	CFArrayRef servers = CFDictionaryGetValue(dns, CFSTR("ServerAddresses"));
	if (!servers || CFArrayGetCount(servers) == 0) {
		CFRelease(dns);
		return NULL;
	}

	CFStringRef first = CFArrayGetValueAtIndex(servers, 0);
	char buf[64];
	if (!CFStringGetCString(first, buf, sizeof(buf), kCFStringEncodingUTF8)) {
		CFRelease(dns);
		return NULL;
	}
	CFRelease(dns);
	return strdup(buf);
}

// k2SetSetupDNS writes Setup:/Network/Service/<id>/DNS = {ServerAddresses: [server]}.
// Setup:/ keys produce resolvers WITHOUT if_index — mDNSResponder uses routing table
// instead of scoped queries, allowing DNS to reach TUN interface.
// Returns 0 on success, -1 on failure.
static int k2SetSetupDNS(const char* serviceID, const char* server) {
	SCDynamicStoreRef store = SCDynamicStoreCreate(NULL, CFSTR("k2-dns"), NULL, NULL);
	if (!store) return -1;

	CFStringRef cfKey = buildDNSKey("Setup:", serviceID);
	CFStringRef cfServer = CFStringCreateWithCString(NULL, server, kCFStringEncodingUTF8);
	const void* serverArr[] = { cfServer };
	CFArrayRef servers = CFArrayCreate(NULL, serverArr, 1, &kCFTypeArrayCallBacks);

	const void* dkeys[] = { CFSTR("ServerAddresses") };
	const void* dvals[] = { servers };
	CFDictionaryRef dns = CFDictionaryCreate(NULL, dkeys, dvals, 1,
		&kCFTypeDictionaryKeyCallBacks, &kCFTypeDictionaryValueCallBacks);

	Boolean ok = SCDynamicStoreSetValue(store, cfKey, dns);

	CFRelease(dns);
	CFRelease(servers);
	CFRelease(cfServer);
	CFRelease(cfKey);
	CFRelease(store);
	return ok ? 0 : -1;
}

// k2RemoveSetupDNS removes Setup:/Network/Service/<id>/DNS.
// After removal, configd falls back to State:/ key (DHCP-assigned DNS).
static void k2RemoveSetupDNS(const char* serviceID) {
	SCDynamicStoreRef store = SCDynamicStoreCreate(NULL, CFSTR("k2-dns"), NULL, NULL);
	if (!store) return;

	CFStringRef cfKey = buildDNSKey("Setup:", serviceID);
	SCDynamicStoreRemoveValue(store, cfKey);
	CFRelease(cfKey);
	CFRelease(store);
}

// k2StartMonitor registers callbacks for:
//   - State:/Network/Global/IPv4 changes (network switch, primary service change)
//   - Setup:/Network/Service/<id>/DNS changes (user manual override in System Preferences)
//   - State:/Network/Service/<id>/DNS changes (DHCP DNS assignment on new service)
// Runs CFRunLoop on the current thread (blocks until k2StopMonitor).
static int k2StartMonitor() {
	SCDynamicStoreContext ctx = {0, NULL, NULL, NULL, NULL};
	g_store = SCDynamicStoreCreate(NULL, CFSTR("k2-dns-monitor"), dnsCallback, &ctx);
	if (!g_store) return -1;

	// Exact key: primary service changes.
	CFStringRef exactKey = CFSTR("State:/Network/Global/IPv4");
	CFArrayRef exactKeys = CFArrayCreate(NULL, (const void**)&exactKey, 1, &kCFTypeArrayCallBacks);

	// Patterns: per-service DNS changes (both Setup:/ and State:/).
	// Setup:/ = user overrides (System Preferences).
	// State:/ = DHCP DNS assignments (network switch creates new service).
	// Split path to avoid Go comment parser seeing star-slash in ".*".
	const char *pat1 = "Setup:/Network/Service/." "*" "/DNS";
	const char *pat2 = "State:/Network/Service/." "*" "/DNS";
	CFStringRef cfPat1 = CFStringCreateWithCString(NULL, pat1, kCFStringEncodingUTF8);
	CFStringRef cfPat2 = CFStringCreateWithCString(NULL, pat2, kCFStringEncodingUTF8);
	const void* patArr[] = { cfPat1, cfPat2 };
	CFArrayRef patterns = CFArrayCreate(NULL, patArr, 2, &kCFTypeArrayCallBacks);

	if (!SCDynamicStoreSetNotificationKeys(g_store, exactKeys, patterns)) {
		CFRelease(cfPat1);
		CFRelease(cfPat2);
		CFRelease(patterns);
		CFRelease(exactKeys);
		CFRelease(g_store);
		g_store = NULL;
		return -2;
	}
	CFRelease(cfPat1);
	CFRelease(cfPat2);
	CFRelease(patterns);
	CFRelease(exactKeys);

	g_source = SCDynamicStoreCreateRunLoopSource(NULL, g_store, 0);
	if (!g_source) {
		CFRelease(g_store);
		g_store = NULL;
		return -3;
	}

	g_runLoop = CFRunLoopGetCurrent();
	CFRunLoopAddSource(g_runLoop, g_source, kCFRunLoopCommonModes);

	// Blocks until CFRunLoopStop() is called from another thread.
	CFRunLoopRun();

	// Cleanup after stop.
	CFRunLoopRemoveSource(g_runLoop, g_source, kCFRunLoopCommonModes);
	CFRelease(g_source);
	CFRelease(g_store);
	g_source = NULL;
	g_store = NULL;
	g_runLoop = NULL;

	return 0;
}

// k2StopMonitor stops the CFRunLoop started by k2StartMonitor.
static void k2StopMonitor() {
	if (g_runLoop) {
		CFRunLoopStop(g_runLoop);
	}
}
*/
import "C"

import (
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync/atomic"

	"github.com/sasha-s/go-deadlock"
	"time"
	"unsafe"
)

// dnsServiceFile returns the path to the file that persists the overridden
// service ID across crashes. Used to clean stale Setup:/ keys on restart.
func dnsServiceFile() string {
	if os.Getuid() == 0 {
		return "/var/run/k2-dns-service"
	}
	if home, err := os.UserHomeDir(); err == nil {
		return filepath.Join(home, ".config", "k2", "dns-service")
	}
	return filepath.Join(os.TempDir(), "k2-dns-service")
}

// CleanupStaleDNSOverride removes any leftover Setup:/ DNS key from a
// previous crash. Called at daemon startup before tunnel connects.
func CleanupStaleDNSOverride() {
	path := dnsServiceFile()
	data, err := os.ReadFile(path)
	if err != nil {
		return // no saved state, nothing to clean
	}
	serviceID := strings.TrimSpace(string(data))
	if serviceID == "" {
		os.Remove(path)
		return
	}

	cid := C.CString(serviceID)
	defer C.free(unsafe.Pointer(cid))

	cstr := C.k2GetSetupDNS(cid)
	if cstr != nil {
		C.free(unsafe.Pointer(cstr))
		C.k2RemoveSetupDNS(cid)
		flushDNSCache()
		slog.Info("provider: cleaned stale DNS override from previous crash", "service", serviceID)
	}
	os.Remove(path)
}

// dnsOverride manages macOS system DNS to point at the TUN address.
//
// Key insight: State:/ per-service DNS keys produce resolvers with if_index bound
// to the physical interface (en0). mDNSResponder sends scoped queries through en0,
// which can never reach the TUN interface. Setup:/ keys produce resolvers WITHOUT
// if_index, so mDNSResponder uses the routing table → packets reach TUN.
//
// Uses SCDynamicStore to write Setup:/Network/Service/<id>/DNS and monitors for
// user overrides (System Preferences) and network changes (service switch).
// On stop, removes the Setup:/ key so DHCP State:/ DNS takes over.
type dnsOverride struct {
	tunDNS    string // target DNS, e.g. "198.18.0.8" (TUN_IP.Next())
	origDNS   string // saved original DNS from State:/ (DHCP-assigned)
	serviceID string // primary service ID being overridden

	mu      deadlock.Mutex
	stopCh  chan struct{}
	stopped chan struct{}
}

// activeOverride is the singleton dnsOverride for the C callback.
var activeOverride atomic.Pointer[dnsOverride]

//export goDNSChanged
func goDNSChanged() {
	slog.Debug("provider: DNS change callback fired")
	if d := activeOverride.Load(); d != nil {
		d.reapply()
	}
}

func newDNSOverride(tunDNS string) *dnsOverride {
	return &dnsOverride{
		tunDNS:  tunDNS,
		stopCh:  make(chan struct{}),
		stopped: make(chan struct{}),
	}
}

// set overrides the primary service's DNS with the TUN address.
// Reads original DNS from State:/ (DHCP), writes override to Setup:/.
func (d *dnsOverride) set() error {
	// Find primary network service.
	csvc := C.k2GetPrimaryServiceID()
	if csvc == nil {
		return fmt.Errorf("no primary network service")
	}
	d.serviceID = C.GoString(csvc)
	C.free(unsafe.Pointer(csvc))

	cid := C.CString(d.serviceID)
	defer C.free(unsafe.Pointer(cid))

	// Clean stale Setup:/ key from a DIFFERENT service (crash + network switch).
	savedPath := dnsServiceFile()
	if data, err := os.ReadFile(savedPath); err == nil {
		oldSvc := strings.TrimSpace(string(data))
		if oldSvc != "" && oldSvc != d.serviceID {
			oldID := C.CString(oldSvc)
			cstr := C.k2GetSetupDNS(oldID)
			if cstr != nil {
				C.free(unsafe.Pointer(cstr))
				C.k2RemoveSetupDNS(oldID)
				slog.Info("provider: cleaned stale DNS override from different service", "old", oldSvc)
			}
			C.free(unsafe.Pointer(oldID))
		}
	}

	// Clean stale Setup:/ key on current service (crash, same service).
	cstr := C.k2GetSetupDNS(cid)
	if cstr != nil {
		stale := C.GoString(cstr)
		C.free(unsafe.Pointer(cstr))
		if stale != d.tunDNS {
			// Not our key, leave it alone (user-configured DNS).
		} else {
			slog.Info("provider: cleaning stale DNS override from previous run")
			C.k2RemoveSetupDNS(cid)
		}
	}

	// Save original DNS from DHCP State:/ key.
	cstr = C.k2GetStateDNS(cid)
	if cstr != nil {
		d.origDNS = C.GoString(cstr)
		C.free(unsafe.Pointer(cstr))
	}

	// Write our DNS to Setup:/ (no if_index → routing table → TUN).
	cs := C.CString(d.tunDNS)
	defer C.free(unsafe.Pointer(cs))
	if ret := C.k2SetSetupDNS(cid, cs); ret != 0 {
		return fmt.Errorf("SCDynamicStoreSetValue failed: %d", ret)
	}

	// Persist service ID for crash recovery.
	if dir := filepath.Dir(savedPath); dir != "" {
		os.MkdirAll(dir, 0o755)
	}
	os.WriteFile(savedPath, []byte(d.serviceID), 0o644)

	flushDNSCache()
	slog.Info("provider: macOS DNS override set", "dns", d.tunDNS, "orig", d.origDNS, "service", d.serviceID)
	return nil
}

// reapply ensures the current primary service's Setup:/ DNS points to TUN.
// Handles user overrides (System Preferences) and network switches.
// Uses read-copy-act: lock only for field access, CGo/I/O outside lock.
func (d *dnsOverride) reapply() {
	// Guard: skip if stop is in progress (handles timer-triggered reapply after stop).
	select {
	case <-d.stopCh:
		return
	default:
	}

	// Step 1: CGo read — no lock.
	csvc := C.k2GetPrimaryServiceID()
	if csvc == nil {
		// Network switch in progress — PrimaryService temporarily NULL.
		// Re-check after configd settles to catch the new service.
		slog.Info("provider: network transition detected, scheduling re-check")
		time.AfterFunc(500*time.Millisecond, d.reapply)
		return
	}
	newSvc := C.GoString(csvc)
	C.free(unsafe.Pointer(csvc))

	// Step 2: Check state under lock.
	d.mu.Lock()
	oldSvc := d.serviceID
	tunDNS := d.tunDNS
	needSwitch := newSvc != oldSvc
	if needSwitch {
		d.serviceID = newSvc
	}
	d.mu.Unlock()

	cid := C.CString(newSvc)
	defer C.free(unsafe.Pointer(cid))

	// Step 3: Handle service switch — all CGo outside lock.
	if needSwitch {
		oldID := C.CString(oldSvc)
		C.k2RemoveSetupDNS(oldID)
		C.free(unsafe.Pointer(oldID))

		// Save new service's DHCP DNS.
		cstr := C.k2GetStateDNS(cid)
		var newOrigDNS string
		if cstr != nil {
			newOrigDNS = C.GoString(cstr)
			C.free(unsafe.Pointer(cstr))
		}

		d.mu.Lock()
		d.origDNS = newOrigDNS
		d.mu.Unlock()

		slog.Info("provider: primary service changed",
			"old", oldSvc, "new", newSvc, "origDNS", newOrigDNS)
		os.WriteFile(dnsServiceFile(), []byte(newSvc), 0o644)
	}

	// Step 4: Check if Setup:/ DNS is still ours — CGo outside lock.
	cstr := C.k2GetSetupDNS(cid)
	if cstr != nil {
		current := C.GoString(cstr)
		C.free(unsafe.Pointer(cstr))
		if current == tunDNS {
			return // still ours, no-op (prevents callback loop)
		}
		slog.Info("provider: DNS overwritten, reapplying", "was", current)
	}

	cs := C.CString(tunDNS)
	defer C.free(unsafe.Pointer(cs))
	C.k2SetSetupDNS(cid, cs)
	flushDNSCache()
	slog.Info("provider: DNS override reapplied", "dns", tunDNS, "service", newSvc)
}

// monitor runs the SCDynamicStore CFRunLoop on a dedicated OS thread.
func (d *dnsOverride) monitor() {
	activeOverride.Store(d)
	defer activeOverride.Store(nil)

	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	// NO recover() here — intentional. C.k2StopMonitor() is a pure CGo call:
	// C-layer crashes (segfault) bypass Go recover entirely (OS signal → process death).
	// Go-layer panics cannot occur (only a channel recv + one CGo call).
	// If k2StopMonitor blocks, dnsOverride.stop() has a 5s timeout fallback.
	go func() {
		<-d.stopCh
		C.k2StopMonitor()
	}()

	if ret := C.k2StartMonitor(); ret != 0 {
		slog.Warn("provider: DNS monitor failed", "ret", ret)
	}
	close(d.stopped)
}

// stop removes the Setup:/ DNS override, restoring DHCP State:/ DNS.
// Lock only for field reads; CGo/I/O outside lock.
func (d *dnsOverride) stop() {
	close(d.stopCh)
	select {
	case <-d.stopped:
		// Normal: CFRunLoop stopped cleanly.
	case <-time.After(5 * time.Second):
		slog.Warn("provider: DNS monitor stop timed out, proceeding with cleanup")
	}

	d.mu.Lock()
	serviceID := d.serviceID
	d.serviceID = ""
	origDNS := d.origDNS
	d.mu.Unlock()

	if serviceID != "" {
		cid := C.CString(serviceID)
		C.k2RemoveSetupDNS(cid)
		C.free(unsafe.Pointer(cid))
	}
	os.Remove(dnsServiceFile())
	flushDNSCache()
	slog.Info("provider: macOS DNS override removed", "restored", origDNS)
}

func flushDNSCache() {
	exec.Command("dscacheutil", "-flushcache").Run()
}

// saveDNSState is a no-op on macOS (uses dnsServiceFile for crash recovery).
func saveDNSState(ifaceName, dnsServer string) {}

// removeDNSState is a no-op on macOS (dnsOverride.stop removes dnsServiceFile).
func removeDNSState() {}
