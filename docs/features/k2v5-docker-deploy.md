# k2v5 Docker Deploy

## Meta
- Status: implemented
- Version: 1.0.0
- Created: 2026-02-20
- Feature: k2v5-docker-deploy

## Context

The existing deployment at `kaitu/docker/` orchestrates k2v4-slave (Rust SNI router, port 443), k2-slave-sidecar (Go config gen + RADIUS + health), k2-slave (legacy v3), and k2-oc (OpenConnect). k2v4-slave owns port 443 and routes ECH traffic to k2v4 protocol, non-ECH to legacy services via `local_routes`.

k2v5 (`k2s` binary from k2/ submodule) is a QUIC+TCP-WS composite VPN with ECH camouflage. It needs to coexist with k2v4-slave and k2-oc on the same server. The key differentiator: **k2v5 uses ECH, k2v4 does not**. This makes ECH presence the natural traffic-splitting signal.

## Architecture

### Port 443 Layered Routing

k2v5 (k2s) takes over port 443 as the front-door SNI router on host network:

```
Internet :443 (TCP + UDP)
    │
    ├─ TCP: k2s peeks TLS ClientHello
    │   ├─ ECH present → k2v5 handler (TLS+WS/QUIC, auth, proxy)
    │   ├─ No ECH, SNI matches local_routes → TCP relay to backend
    │   │   ├─ K2_DOMAIN → k2v4-slave (127.0.0.1:K2V4_PORT)
    │   │   └─ K2OC_DOMAIN → k2-oc (127.0.0.1:K2OC_PORT)
    │   └─ No ECH, unknown SNI → camouflage reverse proxy
    │
    └─ UDP: k2v5 QUIC handler (all UDP belongs to k2v5)
```

**Consequences:**
- k2v4 clients lose QUIC (TCP-WS fallback is sufficient)
- k2v4-slave moves from host network to bridge network (TCP-only, port-mapped)
- Anti-probe camouflage remains fully functional (unknown SNI → real website)
- Jump port DNAT (10020-10119 → 443) done by k2v5 entrypoint

### Docker Compose Services

```
services:
  k2-sidecar       # bridge — config gen, RADIUS, health reporting
  k2v5             # host network — owns :443 TCP+UDP, hop port DNAT
  k2v4-slave       # bridge — port K2V4_PORT:443, k2v4 protocol (TCP-WS)
  k2-oc            # bridge — port K2OC_PORT:443, OpenConnect
  k2-slave         # bridge — port K2V3_PORT:443, legacy v3 (optional)

networks:
  k2-internal (bridge)

volumes:
  config — shared /etc/kaitu + /etc/ocserv
```

**Dependency chain:** k2-sidecar (healthy) → k2v5, k2v4-slave, k2-oc

### Tunnel Domain Model

**Shared domain, protocol=k2v5** — k2v4 and k2v5 share the same domain on the same node.

- Sidecar registers tunnel with `protocol: "k2v5"` to Center API
- DB stores ONE tunnel record per domain (existing unique constraint preserved)
- API backward compatibility: `GET /api/tunnels/k2v4` also returns k2v5 tunnels (since the physical connection is compatible — k2v5 front door forwards non-ECH to k2v4-slave)
- API new endpoint: `GET /api/tunnels/k2v5` returns k2v5 tunnels with ECH config

**Certificate split:**
- k2v5 (k2s): self-signed cert with cert pin (auto-provisioned by k2s, stable 10yr TTL)
- k2v4-slave: Center CA-signed cert (issued by sidecar registration, TLS trust chain)
- No conflict: TLS termination happens at different processes

**ECH keys:** Sidecar fetches from Center → writes `/etc/kaitu/ech_keys.yaml` → k2v5 reads.

## Components

### 1. docker/docker-compose.yml

New compose file for k2v5 deployment. References:
- `k2-sidecar`: built from `./sidecar/`
- `k2v5`: built from `./k2s/`
- `k2v4-slave`: pre-built ECR image (unchanged)
- `k2-oc`: pre-built ECR image (unchanged)
- `k2-slave`: pre-built ECR image (optional, legacy v3)

### 2. docker/sidecar/ — Go sidecar (migrated from kaitu)

Source files migrated from `kaitu/`:
- `cmd/k2-slave-sidecar/main.go` → `docker/sidecar/main.go`
- `server/slave/sidecar/*.go` → `docker/sidecar/sidecar/`
- `server/slave/k2/config.go` → `docker/sidecar/config/`

**Adaptations for k2v5:**
- Register tunnel with protocol="k2v5" (new constant)
- Generate `k2v5-config.yaml` for k2s:
  - `local_routes`: K2_DOMAIN → k2v4-slave, K2OC_DOMAIN → k2-oc
  - `ech.keys_file`: path to ECH keys
  - No cert/key (k2s auto-provisions self-signed)
- Generate `k2v4-config.yaml` for k2v4-slave:
  - `cert_file`/`key_file`: Center CA cert (from registration)
  - No ECH keys (k2v5 handles ECH)
  - No `local_routes` (k2v5 handles routing)
- Keep existing: ocserv config gen, RADIUS proxy, metrics, traffic monitor

### 3. docker/k2s/ — k2v5 server container

- `Dockerfile`: builds `k2s` binary from `k2/` submodule (or copies pre-built)
- `entrypoint.sh`:
  1. Wait for sidecar `.ready` flag
  2. iptables DNAT: hop ports → 443 (TCP+UDP)
  3. iptables: ensure UDP 443 accessible
  4. `exec ./k2s -c /etc/kaitu/k2v5-config.yaml`

### 4. API changes (api/)

Minimal changes for backward compatibility:

- Add `TunnelProtocolK2V5 = "k2v5"` constant in `model.go`
- In `api_tunnel.go`: when protocol=k2v4 requested, also include tunnels with protocol=k2v5
- Add cert pin field to tunnel API response (for k2v5 client connection)
- Sidecar reports cert pin during node registration

### 5. demo.env

```bash
K2_NODE_SECRET=xxx
K2_DOMAIN=*.example.com        # shared domain (k2v4 + k2v5)
K2_PORT=443
K2V4_PORT=8443                  # k2v4-slave bridge port
K2OC_DOMAIN=oc.example.com
K2OC_PORT=10001
K2V3_PORT=                      # empty = no legacy v3
K2_HOP_PORT_MIN=10020
K2_HOP_PORT_MAX=10119
```

## Traffic Flow Examples

### k2v5 client connects (new)
1. Client calls `GET /api/tunnels/k2v5` → gets domain, ECH config, cert pin
2. Client sends TLS ClientHello to domain:443 **with ECH**
3. k2v5 detects ECH → decrypts inner SNI → k2v5 auth + proxy handler
4. QUIC (UDP) or TCP-WS transport

### k2v4 client connects (backward compatible)
1. Client calls `GET /api/tunnels/k2v4` → API returns k2v5 tunnel (compat)
2. Client sends TLS ClientHello to domain:443 **without ECH**
3. k2v5 sees no ECH → checks SNI → matches `local_routes[K2_DOMAIN]`
4. k2v5 does raw TCP relay to 127.0.0.1:K2V4_PORT
5. k2v4-slave terminates TLS (Center CA cert) → k2v4 protocol handler

### Probe / censorship detection
1. Probe sends TLS ClientHello to domain:443 **without ECH**, unknown SNI
2. k2v5 sees no ECH → SNI not in local_routes → camouflage reverse proxy
3. Probe sees real website content, indistinguishable from normal HTTPS

### k2-oc client connects
1. k2-oc client connects to K2OC_DOMAIN:443
2. k2v5 sees no ECH → SNI matches `local_routes[K2OC_DOMAIN]`
3. k2v5 does raw TCP relay to 127.0.0.1:K2OC_PORT
4. k2-oc container handles OpenConnect protocol

## Acceptance Criteria

- [ ] AC1: docker-compose up brings up all 4 services (sidecar, k2v5, k2v4-slave, k2-oc) with correct dependency order
- [ ] AC2: k2v5 owns port 443 TCP+UDP on host; k2v4-slave and k2-oc are on bridge network
- [ ] AC3: ECH connections to :443 are handled by k2v5 protocol
- [ ] AC4: Non-ECH connections with K2_DOMAIN SNI are forwarded to k2v4-slave
- [ ] AC5: Non-ECH connections with K2OC_DOMAIN SNI are forwarded to k2-oc
- [ ] AC6: Unknown SNI without ECH triggers camouflage reverse proxy
- [ ] AC7: Hop ports (10020-10119) redirect to 443 via iptables DNAT (TCP+UDP)
- [ ] AC8: Sidecar generates correct configs: k2v5-config.yaml (with local_routes + ECH), k2v4-config.yaml (with Center cert, no ECH)
- [ ] AC9: Sidecar registers tunnel with protocol=k2v5 to Center
- [ ] AC10: API `GET /api/tunnels/k2v4` returns k2v5 tunnels for backward compatibility
- [ ] AC11: API `GET /api/tunnels/k2v5` returns k2v5 tunnels with ECH config
- [ ] AC12: Sidecar RADIUS proxy works for k2-oc authentication
- [ ] AC13: Health reporting and traffic monitoring work correctly

## Migration Path

**Existing nodes (k2v4-only) → k2v5 deployment:**

1. Stop current docker-compose
2. Deploy new docker-compose (from k2app/docker/)
3. Sidecar re-registers with protocol=k2v5 (overwrites old k2v4 record)
4. k2v4 clients continue working (forwarded by k2v5)
5. k2v5 clients can now connect

**Future k2v4 sunset:**

1. Remove k2v4-slave service from docker-compose
2. Remove K2_DOMAIN from local_routes
3. Old k2v4 clients get camouflage response (graceful degradation)

## Version History

| Version | Date | Changes |
|---------|------|---------|
| 1.0.0 | 2026-02-20 | Initial spec: k2v5 front door + sidecar migration + API compat |
