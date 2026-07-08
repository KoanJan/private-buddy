//go:build linux && 386

// Package sandbox provides cross-platform kernel-level sandbox execution.
// This file embeds the bubblewrap binary for linux/386.
package sandbox

import _ "embed"

//go:embed bwrap/bwrap_linux_386
var bwrapBinary []byte
