//go:build (darwin && !ios) || (linux && !android) || windows

package provider

// Desktop has no platform MTU cap.
const platformMaxMTU = 0
