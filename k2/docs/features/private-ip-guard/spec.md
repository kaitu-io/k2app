# Feature Spec: Private IP Guard

## Problem

Server `ProxyHandler` dials any address the client requests — including private/LAN IPs.
A malicious client can use the server as an SSRF proxy to reach internal services
(localhost, 10.x, 172.16.x, 192.168.x, etc.) behind the server's network.

## Solution

Block outbound connections to reserved IP ranges in `ProxyHandler` before dialing.

## Blocked Ranges

| Range | Description |
|-------|-------------|
| 127.0.0.0/8, ::1 | Loopback |
| 10.0.0.0/8 | RFC 1918 private |
| 172.16.0.0/12 | RFC 1918 private |
| 192.168.0.0/16 | RFC 1918 private |
| 169.254.0.0/16, fe80::/10 | Link-local |
| fc00::/7 | IPv6 unique local |
| 0.0.0.0, :: | Unspecified |
| 100.64.0.0/10 | CGN (shared address space) |

Go stdlib covers most of these: `IsPrivate()`, `IsLoopback()`, `IsLinkLocalUnicast()`, `IsUnspecified()`.
CGN (100.64.0.0/10) requires manual check.

## Acceptance Criteria

- AC1: TCP proxy to private IPs returns error, connection is not established
- AC2: UDP proxy to private IPs returns error, connection is not established
- AC3: TCP/UDP proxy to public IPs works unchanged
- AC4: Domain names resolving to private IPs are blocked (DNS resolution before dial)
