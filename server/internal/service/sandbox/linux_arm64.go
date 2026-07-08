//go:build linux && arm64

// Package sandbox provides cross-platform kernel-level sandbox execution.
// This file embeds the bubblewrap binary for linux/arm64.
package sandbox

import _ "embed"

//go:embed bwrap/bwrap_linux_arm64
var bwrapBinary []byte
