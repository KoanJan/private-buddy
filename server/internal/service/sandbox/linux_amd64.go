//go:build linux && amd64

// Package sandbox provides cross-platform kernel-level sandbox execution.
// This file embeds the bubblewrap binary for linux/amd64.
package sandbox

import _ "embed"

//go:embed bwrap/bwrap_linux_amd64
var bwrapBinary []byte
