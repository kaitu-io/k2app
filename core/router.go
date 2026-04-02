package core

import (
	"github.com/kaitu-io/k2/core/packet"
	"github.com/kaitu-io/k2/rule"
)

// Router implements routing decisions via the rule engine.
type Router struct {
	engine *rule.Engine
}

// NewRouter creates a new router backed by the given rule engine.
func NewRouter(engine *rule.Engine) *Router {
	return &Router{engine: engine}
}

// Route makes a routing decision for a packet.
func (r *Router) Route(info packet.Info) rule.Target {
	return r.engine.Match(info.DstIP)
}

// RouteAddr makes a routing decision for a host address (IP or domain).
func (r *Router) RouteAddr(host string) rule.Target {
	return r.engine.Match(host)
}

// SetTmpRule adds a DNS-learned temporary routing rule.
func (r *Router) SetTmpRule(ip string, target rule.Target) {
	r.engine.SetTmpRule(ip, target)
}
