//go:build !linux || (linux && !386 && !amd64 && !arm && !arm64)

// Package sandbox provides cross-platform kernel-level sandbox execution.
// On non-Linux platforms or unsupported Linux architectures, bwrapBinary
// is nil — BwrapAvailable will return false and runLinux will fall back
// to plain exec.
package sandbox

// bwrapBinary is nil on unsupported platforms. bwrapLookup will return
// an error, causing the sandbox to fall back to plain exec gracefully.
var bwrapBinary []byte
