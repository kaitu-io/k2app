package rule

// MatchDNSMeta returns the target of the first PURELY-PROCESS route matching
// this connection metadata. It exists for the DNS pipeline's per-process
// resolver-view override (App Bypass: a force-direct app's domains must
// resolve through direct DNS, not the node's view — and symmetrically for
// force-proxy), where the only trustworthy dimensions are the querying
// process's identity fields.
//
// Participation is deliberately narrower than the connection-time pre-tmp
// scan in matchConnLocked:
//
//   - routes with host criteria never participate (the domain half already
//     had its say via Match);
//   - routes WITHOUT process criteria never participate — a {port} or
//     {network} route describes the app's DATA connections, and the DNS
//     query's own port/transport (53/udp) must not stand in for them.
//
// Callers therefore pass a ConnMeta carrying ONLY process identity
// (ProcessName / PackageName / InstallerPackage); every other dimension is
// left at its fail-closed zero value, so a combined route like
// {apps, port: 443} correctly refuses to match here (matchMeta's AND
// semantics see Port==0).
//
// The boolean reports whether any route matched; the Target is only
// meaningful when it is true.
func (e *Engine) MatchDNSMeta(meta ConnMeta) (Target, bool) {
	if meta.ProcessName == "" && meta.ProcessPath == "" && meta.PackageName == "" && len(meta.PackageNames) == 0 && meta.InstallerPackage == "" {
		return 0, false
	}
	e.mu.RLock()
	t, ok := e.matchDNSMetaLocked(meta)
	e.mu.RUnlock()
	return t, ok
}

func (e *Engine) matchDNSMetaLocked(meta ConnMeta) (Target, bool) {
	for i := range e.routes {
		r := &e.routes[i]
		if r.hasHostCriteria() || !r.hasProcessCriteria() {
			continue
		}
		if matchMeta(r, meta) {
			return r.Target, true
		}
	}
	return 0, false
}
