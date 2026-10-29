# .github — Workflows

- **Release-tag suffixes live only in `scripts/ci/release-plan.mjs`** (the `plan` jobs of `release-desktop.yml` and `build-mobile.yml` call it). Edit the script, never re-inline a bash suffix table or `TAG_VERSION%-` strip list in YAML — `release-plan.test.mjs` (`ci.yml` `test-ci-scripts`) fails if you do. Web OTA tags (`webapp/*`) are parsed separately by `scripts/ci/web-ota-plan.mjs`.
- **`ci.yml` job commands are mirrored in `scripts/ci/k2ci-run.sh`** (the labtest pre-merge suites). Change a job's commands → change its suite there too; otherwise pre-merge and post-merge test different things. After editing `.labtest.yml` or `k2ci-run.sh`, run labtest with `all`.
- The "Remove legacy k2 submodule checkout (self-hosted workspace)" steps in `ci.yml` and `release-desktop.yml` only clean submodule-era leftovers from self-hosted runners' persisted workspaces (no-op on a clean one). Delete them once every self-hosted runner has run them.
- Script inventory and which workflow calls what: `scripts/CLAUDE.md`.
