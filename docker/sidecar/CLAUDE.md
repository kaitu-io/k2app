# docker/sidecar — k2-sidecar (node metering + quota cutoff)

Standalone Go module `github.com/kaitu-io/k2-sidecar` (does not import k2) running next to k2s on every node: registers the node with Center, generates the tunnel config, meters host-NIC traffic, reports usage, and enforces the quota cutoff by **pausing the data-plane containers** through the Docker API (`sidecar/enforcer.go`, `docker_control.go`).

- Test: `cd docker/sidecar && go test ./...`. Build: `CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o k2-sidecar .` (output gitignored).
- Ship: `make publish-docker` (`scripts/publish-docker.sh`) builds **both** k2s and k2-sidecar and pushes `:latest` + `:vX.Y.Z-<commit>` of each to `public.ecr.aws/d6n9t2r2` — publishing one image publishes the other too.
- **No test gate**: no `.labtest.yml` suite or CI job covers this directory — run `go test ./...` yourself.
- **Config comes from env via `entrypoint.sh`**, which writes the YAML that `config/config.go` reads; only a few fields (`K2_JUMP_PORT_*`, `K2_IP_TYPE`) are also read from env directly. A new config field needs a line in that template, or its env var never arrives.
- **`quotaCutoffReserveBytes` (500 MiB) must equal Center's copy** in `api/logic_node_usage.go` — two modules, no shared code or test; drift makes node-side and Center-side cutoffs disagree.
- The enforcer fails closed: after 3 consecutive meter-read errors with a known limit it pauses traffic. Cutoff state is persisted, so a restart mid-cut re-applies it.
- `k2-sidecar -c <cfg> set-usage <GB>` rewrites the persisted baseline; restart the sidecar to load it.

Fleet operation, env vars, Lightsail billing rules and troubleshooting: project skill `kaitu-node-ops` (`.claude/skills/kaitu-node-ops/`).
