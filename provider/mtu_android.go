//go:build android

package provider

// Android VpnService with MTU 65535 triggers ENOBUFS on some devices.
const platformMaxMTU = 9000
