package wire

import (
	"log/slog"
	"math/rand/v2"
	"os"
	"sync"

	"github.com/kaitu-io/k2/wire/k2cc"
)

// ccVariantsEnv selects the server-side K2CC A/B split, e.g.
// "control:50,fix-a3:25,fix-a15:25". Unset or invalid = 100 % control, so
// shipping the binary changes nothing until a node opts in. Read once at
// first accept; changing it needs a k2s restart. Server (download) senders
// only — the client and k2p senders are not part of the experiment.
const ccVariantsEnv = "K2_CC_VARIANTS"

var ccVariantPicker = sync.OnceValue(func() *k2cc.VariantPicker {
	return loadCCVariants(os.Getenv(ccVariantsEnv))
})

func loadCCVariants(spec string) *k2cc.VariantPicker {
	p, err := k2cc.ParseVariants(spec)
	if err != nil {
		slog.Warn("wire: "+ccVariantsEnv+" invalid, using control for every connection", "value", spec, "err", err)
		p, _ = k2cc.ParseVariants("")
	}
	if spec != "" {
		slog.Info("wire: K2CC variants", "split", p.String())
	}
	return p
}

func pickCCVariant() string {
	return ccVariantPicker().Pick(rand.Float64())
}
