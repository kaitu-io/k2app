//go:build ios

package provider

// Apple NetworkExtension has a kernel bug that causes severe performance
// degradation with MTU > 4064 (4096 - UTUN_IF_HEADROOM_SIZE).
const platformMaxMTU = 4064
