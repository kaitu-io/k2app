//go:build ios || android

package config

func init() {
	DefaultTunIPv4 = "10.0.0.2/24"
	DefaultTunIPv6 = "fd00::2/64"
}
