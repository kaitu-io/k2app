package core

import (
	"net"

	"github.com/kaitu-io/k2rule"
	"github.com/kaitu-io/k2tunnel/core/dns"
	"github.com/kaitu-io/k2tunnel/core/packet"
)

// Router implements routing decisions with DNS cache + k2rule
type Router struct {
	dnsCache *dns.DynamicIPCache
}

// NewRouter creates a router with dynamic DNS cache support
func NewRouter(dnsCache *dns.DynamicIPCache) *Router {
	return &Router{
		dnsCache: dnsCache,
	}
}

// Route makes a routing decision for a packet
func (r *Router) Route(info packet.Info) k2rule.Target {
	// 1. Check dynamic cache first (from DNS middleware)
	if r.dnsCache != nil {
		if _, target, found := r.dnsCache.Get(info.DstIP); found {
			return target
		}
	}

	// 2. Check k2rule (global, includes IP/CIDR matching + fallback)
	ip := net.ParseIP(info.DstIP)
	if ip == nil {
		return k2rule.TargetProxy // Default if IP parse fails
	}

	return k2rule.MatchIP(ip)
}
