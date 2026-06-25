package server

import (
	"fmt"
	"net"
)

// validateControlPlaneRoutes rejects malformed or unsafe control-plane routes.
// Origins must be host:port. IP-literal origins that are loopback, private,
// link-local, or unspecified are rejected to prevent a poisoned config from
// turning the node into an open proxy to internal services.
func validateControlPlaneRoutes(routes map[string]string) error {
	for sni, origin := range routes {
		if sni == "" || origin == "" {
			return fmt.Errorf("control_plane_routes: empty sni or origin (sni=%q origin=%q)", sni, origin)
		}
		host, port, err := net.SplitHostPort(origin)
		if err != nil {
			return fmt.Errorf("control_plane_routes[%s]: origin must be host:port: %w", sni, err)
		}
		if host == "" || port == "" {
			return fmt.Errorf("control_plane_routes[%s]: origin missing host or port: %q", sni, origin)
		}
		if ip := net.ParseIP(host); ip != nil {
			if ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() || ip.IsUnspecified() || ip.IsLinkLocalMulticast() {
				return fmt.Errorf("control_plane_routes[%s]: origin IP %s not allowed (loopback/private/link-local)", sni, host)
			}
		}
	}
	return nil
}
