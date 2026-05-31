package rule

import _ "embed"

// embeddedRules is the gzipped tarball of all .krs rule bundles, baked into
// the binary at build time. The build pipeline (`make fetch-rules-embed`)
// overwrites rule/embed/all.krs.tar.gz with the full production archive before
// a release build; the committed copy is a small cn-baseline placeholder so the
// package always compiles and dev/test builds have real bundles.
//
// seedFromEmbed (ensure.go) extracts this into the cache on true cold start —
// instant, offline, censorship-proof. The CDN remains the source of newer
// rules via the background updater.
//
//go:embed embed/all.krs.tar.gz
var embeddedRules []byte
