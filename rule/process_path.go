package rule

import "strings"

// NormalizeProcessPath puts an executable path into the form both sides of
// a ProcessPathPrefixes comparison use, for the given GOOS:
//
//   - windows: backslashes, lowercased (NTFS paths are case-insensitive and
//     the registry, the shell and QueryFullProcessImageName disagree on case);
//   - everything else: unchanged. macOS volumes are usually case-insensitive
//     too, but both sides come from the same on-disk spelling (readdir on the
//     rule side, proc_pidpath on the observed side), and a case-sensitive
//     volume must not be matched loosely.
func NormalizeProcessPath(goos, path string) string {
	if goos == "windows" {
		return strings.ToLower(strings.ReplaceAll(path, "/", `\`))
	}
	return path
}

// NormalizeProcessDir normalizes a directory for use as a
// ProcessPathPrefixes entry: NormalizeProcessPath plus exactly one trailing
// separator, so the prefix test is a path-component test
// ("/Applications/A.app/" does not match "/Applications/A.app2/x").
//
// Returns "" for a directory that must never become a prefix: empty,
// relative, or a filesystem / drive root — any of those would capture every
// process on the machine.
func NormalizeProcessDir(goos, dir string) string {
	sep := "/"
	if goos == "windows" {
		sep = `\`
	}
	p := strings.TrimRight(NormalizeProcessPath(goos, strings.TrimSpace(dir)), sep)
	if p == "" {
		return ""
	}
	if goos == "windows" {
		// "c:" (drive root) or anything not drive- or UNC-anchored.
		if len(p) < 3 || !(p[1] == ':' && p[2] == '\\' || strings.HasPrefix(p, `\\`)) {
			return ""
		}
	} else if !strings.HasPrefix(p, "/") {
		return ""
	}
	return p + sep
}
