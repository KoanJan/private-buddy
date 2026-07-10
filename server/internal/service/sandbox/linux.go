package sandbox

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"sync"
	"time"

	applogger "private-buddy-server/internal/logger"
)

// bwrap path cache — extracted once per process lifetime.
var (
	bwrapPath     string
	bwrapPathOnce sync.Once
	bwrapPathErr  error
)

// bwrapLookup returns the path to the embedded bwrap binary.
// On first call, the binary is extracted from the embedded data to a temp file
// and made executable. Subsequent calls return the cached path.
//
// Returns an error if the embedded binary is not available or extraction fails.
func bwrapLookup() (string, error) {
	bwrapPathOnce.Do(func() {
		if len(bwrapBinary) == 0 {
			bwrapPathErr = fmt.Errorf("bwrap binary not embedded for %s/%s (build bwrap from source for this architecture)", runtime.GOOS, runtime.GOARCH)
			return
		}
		f, err := os.CreateTemp("", "pb-bwrap-*")
		if err != nil {
			bwrapPathErr = fmt.Errorf("create bwrap temp file: %w", err)
			return
		}
		defer f.Close()
		if _, err := f.Write(bwrapBinary); err != nil {
			bwrapPathErr = fmt.Errorf("write bwrap binary: %w", err)
			return
		}
		if err := os.Chmod(f.Name(), 0700); err != nil {
			bwrapPathErr = fmt.Errorf("chmod bwrap: %w", err)
			return
		}
		bwrapPath = f.Name()
		applogger.Info("sandbox: extracted embedded bwrap",
			"path", bwrapPath, "size_bytes", len(bwrapBinary))
	})
	return bwrapPath, bwrapPathErr
}

// BwrapAvailable checks whether bubblewrap is available and can create
// user namespaces. Returns true if the sandbox can be used.
//
// Uses the embedded bwrap binary extracted at runtime. Detection uses a
// 5-second timeout to prevent hanging if bwrap is blocked by security policy.
func BwrapAvailable() (bool, error) {
	path, err := bwrapLookup()
	if err != nil {
		return false, err
	}

	// Directly test namespace creation — the most reliable detection
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, path, "--ro-bind", "/", "/", "true")
	if err := cmd.Run(); err != nil {
		return false, fmt.Errorf("bwrap namespace creation failed: %w (check kernel.unprivileged_userns_clone and apparmor_restrict_unprivileged_userns)", err)
	}
	return true, nil
}

// runLinux executes the command inside a bubblewrap sandbox.
//
// The sandbox uses user namespaces + mount namespaces for isolation:
//   - /usr is mounted read-only
//   - FHS symlinks (/bin, /lib, etc.) are detected and recreated
//   - /etc essential files are bound read-only
//   - Workspace is bound read-write
//   - /tmp, /var/tmp, /dev/shm are tmpfs
//   - Process isolation: new session, PID namespace, IPC namespace, drop all caps
//   - Network: shared with host
//
// Uses the embedded bwrap binary. Falls back to plain exec if bwrap is unavailable.
func runLinux(workspace string, cmd []string) (*exec.Cmd, bool, error) {
	path, err := bwrapLookup()
	if err != nil {
		applogger.Error("sandbox: bwrap unavailable, falling back to plain exec",
			"error", err)
		return fallbackExec(cmd), false, nil
	}

	available, err := BwrapAvailable()
	if err != nil {
		applogger.Error("sandbox: bwrap unavailable, falling back to plain exec",
			"error", err)
		return fallbackExec(cmd), false, nil
	}
	if !available {
		applogger.Error("sandbox: bwrap not available, falling back to plain exec")
		return fallbackExec(cmd), false, nil
	}

	var args []string

	// --- Read-only mount /usr ---
	args = append(args, "--ro-bind", "/usr", "/usr")

	// --- Detect merged-usr vs traditional FHS ---
	// merged-usr distributions (modern Debian/Ubuntu/Arch) have /bin, /lib,
	// /lib64 as symlinks to /usr/... — must use --symlink, not --ro-bind.
	fhsPaths := []struct {
		path   string
		target string // Expected symlink target (relative to /)
	}{
		{"/bin", "usr/bin"},
		{"/lib", "usr/lib"},
		{"/lib64", "usr/lib64"},
		{"/lib32", "usr/lib32"},
		{"/libx32", "usr/libx32"},
	}
	for _, p := range fhsPaths {
		if target, err := os.Readlink(p.path); err == nil {
			// Is a symlink: if merged-usr style, recreate with --symlink
			if target == p.target || target == "/"+p.target {
				args = append(args, "--symlink", p.target, p.path)
			} else {
				// Non-standard symlink: bind if exists
				if _, statErr := os.Stat(p.path); statErr == nil {
					args = append(args, "--ro-bind", p.path, p.path)
				} else if !os.IsNotExist(statErr) {
					applogger.Error("sandbox: stat %s failed: %v (skipped)", p.path, statErr)
				}
			}
		} else if _, statErr := os.Stat(p.path); statErr == nil {
			// Real directory: read-only bind
			args = append(args, "--ro-bind", p.path, p.path)
		} else if !os.IsNotExist(statErr) {
			applogger.Error("sandbox: stat %s failed: %v (skipped)", p.path, statErr)
		}
	}

	// --- System config files (bind if they exist) ---
	for _, p := range []string{
		"/etc/alternatives",
		"/etc/ssl",
		"/etc/ca-certificates",
		"/etc/pki",
		"/etc/resolv.conf",
		"/etc/hosts",
		"/etc/hostname",
		"/etc/passwd",
		"/etc/group",
		"/etc/nsswitch.conf",
		"/etc/localtime",
	} {
		if _, err := os.Stat(p); err == nil {
			args = append(args, "--ro-bind", p, p)
		} else if !os.IsNotExist(err) {
			applogger.Error("sandbox: stat %s failed: %v (skipped)", p, err)
		}
	}

	// --- Read-write workspace ---
	args = append(args, "--bind", workspace, workspace)

	// --- Basic devices and proc ---
	args = append(args, "--dev", "/dev", "--proc", "/proc")

	// --- Temporary filesystems (after --dev to override /dev/shm) ---
	args = append(args,
		"--tmpfs", "/tmp",
		"--tmpfs", "/var/tmp",
		"--tmpfs", "/dev/shm",
	)

	// --- Network ---
	args = append(args, "--share-net")

	// --- Process isolation ---
	args = append(args,
		"--new-session",
		"--unshare-pid",
		"--unshare-ipc",
		"--cap-drop", "ALL",
	)

	// --- Command ---
	args = append(args, "--")
	args = append(args, cmd...)

	applogger.Debug("sandbox: active — bubblewrap")
	return exec.Command(path, args...), true, nil
}
