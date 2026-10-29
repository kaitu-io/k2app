# tessera — Tessera protocol proving ground

Isolated module (`github.com/kaitu-io/tessera`) where risky Tessera mechanisms are built and proven before being **ported** into k2 `wire/`. Not shipped. Design: `k2/docs/tessera/`; details: `README.md`.

- **Zero dependency on k2, on purpose**: `go.mod` `replace`s `apernet/quic-go` with a patched fork, and `replace` applies to the main module's whole build graph — inside k2 it would put the live k2v5 QUIC path on an unproven fork.
- **`make fork` first**: `.fork/` (patched quic-go) is gitignored and materialized from the module cache by `scripts/fork-quic-go.sh`; every other target needs it. The deliverable is `patches/quic-go-utls-seam.patch` — regenerate with `make patch`, never hand-edit. Keep the quic-go version in `go.mod` and the script in sync.
- `make test` (this module, runs with `-ldflags=-checklinkname=0`) · `make test-fork` (quic-go's own suite = regression evidence for the patch; a known set of timing tests fails upstream too — attribute by rerunning on `.fork/quic-go.orig`) · `make build` / `make vet`.
- **No test gate**: no `.labtest.yml` suite or CI job covers `tessera/` — run `make test` yourself.
