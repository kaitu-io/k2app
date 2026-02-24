//go:build !darwin

package appext

// isNetworkExtension is false on non-Apple platforms (Android).
const isNetworkExtension = false
