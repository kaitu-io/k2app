package core

import (
	"github.com/kaitu-io/k2rule"
	"github.com/kaitu-io/k2/core/packet"
)

// Router implements routing decisions via k2rule (static rules + tmp rules from DNS)
type Router struct{}

// NewRouter creates a new router.
func NewRouter() *Router {
	return &Router{}
}

// Route makes a routing decision for a packet.
func (r *Router) Route(info packet.Info) k2rule.Target {
	return k2rule.Match(info.DstIP)
}

// RouteAddr makes a routing decision for a host address (IP or domain).
func (r *Router) RouteAddr(host string) k2rule.Target {
	return k2rule.Match(host)
}
