//go:build linux && arm

// Package sandbox provides cross-platform kernel-level sandbox execution.
// This file embeds the bubblewrap binary for linux/arm (32-bit ARM, e.g. Raspberry Pi).
package sandbox

import _ "embed"

//go:embed bwrap/bwrap_linux_arm
var bwrapBinary []byte
