# k2v5 Connect-URL Delivery Fix

## Problem

k2v5 connection is broken end-to-end due to three issues:

1. **Path mismatch**: k2v5 writes `connect-url.txt` to `/etc/k2s/` (its own ephemeral volume), but sidecar reads from `/etc/kaitu/` (shared config volume). The file is never found.

2. **Timing**: Sidecar registers with Center before k2v5 starts. Since k2v5 hasn't started yet, `connect-url.txt` doesn't exist at registration time. Sidecar never re-checks, so serverUrl is permanently empty in Center DB.

3. **Dead TLS config**: k2v5 config template includes `tls.cert` and `tls.key` entries pointing to sidecar-generated certs, but k2v5 generates its own self-signed certs internally and ignores these entries. The template entries are misleading.

## Solution

### AC1: Separate Docker volume for k2v5 data exchange

Add a `k2v5-data` Docker volume to bridge the k2v5 ↔ sidecar data gap.

- k2v5 container: mount `k2v5-data:/etc/k2v5` (writable, k2v5 writes connect-url.txt here)
- sidecar container: mount `k2v5-data:/etc/k2v5:ro` (read-only, sidecar reads connect-url.txt from here)
- Keep existing `config:/etc/kaitu:ro` mount on k2v5 (unchanged, for reading k2v5-config.yaml)

**Verification**: `docker compose config` shows both volumes mounted correctly on both containers.

### AC2: Background goroutine polls for connect-url.txt and re-registers

After sidecar creates the `.ready` flag, spawn a background goroutine that:

1. Polls `/etc/k2v5/connect-url.txt` every 5 seconds
2. When found: calls `BuildServerURL()` to construct the serverUrl
3. Re-registers with Center using existing `nodeInstance.Register(tunnels)` (with updated ServerURL in the k2v5 TunnelConfig)
4. Logs "Updated k2v5 serverURL: <url>" and exits the goroutine

The goroutine logs progress while waiting (every 30s: "Waiting for k2v5 connect-url.txt...").

If the file is never found, the goroutine runs indefinitely but is harmless (5s sleep loop).

**Verification**: Sidecar logs show "Updated k2v5 serverURL" message after k2v5 starts. Center API returns non-empty serverUrl for the tunnel.

### AC3: Update k2v5 config template — add cert_dir, annotate dormant tls

- Add `cert_dir: "/etc/k2v5"` to the k2v5-config.yaml template (tells k2v5 where to write its self-signed certs and connect-url.txt)
- Keep `tls.cert` and `tls.key` entries but add a comment explaining they are dormant (k2v5 generates its own certs)
- In `saveCertificates()`: skip certificate generation/saving for k2v5 tunnels (k2v5 protocol doesn't use sidecar-generated certs)
- Update connect-url.txt read path in `buildTunnelConfigs()` from `s.config.ConfigDir` to `/etc/k2v5`

**Verification**: `go build` succeeds. Generated k2v5-config.yaml contains `cert_dir`. No cert files are generated for k2v5 domain.

## Technical Decisions

### Decision 1: Separate Docker volume (scrum-validated)

A new `k2v5-data` volume is the cleanest separation. k2v5 owns its writable directory, sidecar reads from it. No changes to k2v5's internal config needed — just mount its data dir to a shared volume.

### Decision 2: Background poll-and-register-once (scrum-validated)

A goroutine that polls and re-registers is simpler than filesystem watchers or IPC. The goroutine exits after one successful registration, so it's fire-and-forget. Re-using `nodeInstance.Register()` ensures the same code path as initial registration.

### Decision 3: cert_dir + dormant tls annotation (scrum-validated)

Adding `cert_dir` tells k2v5 where to write its self-signed certs and connect-url.txt. The dormant `tls.cert/key` entries stay for documentation but are clearly annotated. Sidecar skips cert generation for k2v5 since k2v5 manages its own TLS.

## Files to Modify

| File | Change |
|------|--------|
| `docker/docker-compose.yml` | Add `k2v5-data` volume, mount for k2v5 + sidecar |
| `docker/sidecar/main.go` | Add connect-url poll goroutine after `.ready` flag |
| `docker/sidecar/main.go` | Skip k2v5 cert generation in `saveCertificates()` |
| `docker/sidecar/main.go` | Update k2v5 config template (add `cert_dir`, comment tls) |
| `docker/sidecar/main.go` | Update connect-url.txt read path to `/etc/k2v5` |

## Out of Scope

- k2v5 binary changes (k2v5 is a separate image, no source in this repo)
- Center API changes (already supports serverUrl field)
- Client-side changes (webapp already handles serverUrl from tunnel list)
