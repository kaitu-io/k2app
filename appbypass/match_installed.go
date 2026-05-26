package appbypass

import "strings"

// MatchableApp is the decoupled input shape for MatchInstalled. The daemon
// converts provider.InstalledApp → MatchableApp at the call site so this
// package doesn't import k2/provider (keeps the layer boundary clean — the
// match function stays a pure data transform on local types).
type MatchableApp struct {
	ID                   string
	Label                string
	InstallerPackageName string   // android only
	ProcessNames         []string // desktop: lower-cased
}

// MatchedEntry is one preview result row. HitKind is one of:
//
//	process_exact, process_prefix, package_exact, package_prefix, installer_exact
type MatchedEntry struct {
	ID         string
	Label      string
	Names      []string // the ProcessNames or [packageName] that matched
	HitKind    string
	HitPattern string
}

// MatchInstalled runs the preset's patterns over apps and returns one
// MatchedEntry per app that hits. First-hit-wins ordering inside one app:
//
//	installer_exact > package_exact > package_prefix > process_exact > process_prefix
//
// goos selects which pattern subset is relevant:
//
//	"darwin" / "linux" / "windows": only desktop patterns
//	"android": only android patterns
//	other:    nothing matches
func MatchInstalled(preset *Preset, apps []MatchableApp, goos string) []MatchedEntry {
	if preset == nil || len(apps) == 0 {
		return nil
	}
	useDesktop := goos == "darwin" || goos == "linux" || goos == "windows"
	useAndroid := goos == "android"
	if !useDesktop && !useAndroid {
		return nil
	}
	out := make([]MatchedEntry, 0, len(apps))
	for _, a := range apps {
		var (
			hitKind, hitPattern string
			names               []string
		)
		if useAndroid {
			// installer_exact
			for _, p := range preset.Android.InstallerExact {
				if a.InstallerPackageName == p {
					hitKind, hitPattern = "installer_exact", p
					break
				}
			}
			// package_exact
			if hitKind == "" {
				for _, p := range preset.Android.PackageExact {
					if a.ID == p {
						hitKind, hitPattern = "package_exact", p
						break
					}
				}
			}
			// package_prefix
			if hitKind == "" {
				for _, p := range preset.Android.PackagePrefix {
					if strings.HasPrefix(a.ID, p) {
						hitKind, hitPattern = "package_prefix", p
						break
					}
				}
			}
			if hitKind != "" {
				names = []string{a.ID}
			}
		}
		if useDesktop {
			// process_exact — preset entries already lower-cased at compile time
			for _, p := range preset.Desktop.ProcessExact {
				for _, name := range a.ProcessNames {
					if name == p {
						hitKind, hitPattern = "process_exact", p
						names = append(names, name)
					}
				}
				if hitKind != "" {
					break
				}
			}
			// process_prefix
			if hitKind == "" {
				for _, p := range preset.Desktop.ProcessPrefix {
					for _, name := range a.ProcessNames {
						if strings.HasPrefix(name, p) {
							hitKind, hitPattern = "process_prefix", p
							names = append(names, name)
						}
					}
					if hitKind != "" {
						break
					}
				}
			}
		}
		if hitKind == "" {
			continue
		}
		out = append(out, MatchedEntry{
			ID:         a.ID,
			Label:      a.Label,
			Names:      names,
			HitKind:    hitKind,
			HitPattern: hitPattern,
		})
	}
	return out
}
