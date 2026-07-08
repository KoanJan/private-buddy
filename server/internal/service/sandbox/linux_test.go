package sandbox

import (
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// requireLinux skips the test if not running on Linux.
func requireLinux(t *testing.T) {
	t.Helper()
	if runtime.GOOS != "linux" {
		t.Skip("test requires Linux")
	}
}

func TestBwrapAvailable_NotInstalled(t *testing.T) {
	requireLinux(t)

	available, err := BwrapAvailable()
	if err != nil {
		// bwrap not functional — should not report available
		if available {
			t.Error("BwrapAvailable() returned error but also returned available=true")
		}
		return
	}
	// bwrap is functional — must report available
	if !available {
		t.Error("BwrapAvailable() returned no error but available=false")
	}
}

func TestBwrapLookup_EmbeddedOrFallback(t *testing.T) {
	requireLinux(t)

	path, err := bwrapLookup()
	if err != nil {
		t.Logf("bwrapLookup failed (placeholder binary or unsupported arch): %v", err)
		return
	}
	if path == "" {
		t.Error("expected non-empty path from bwrapLookup")
	}
	t.Logf("bwrap extracted to: %s", path)
}

func TestRunLinux_EmptyCmd(t *testing.T) {
	requireLinux(t)

	_, _, err := Run("/tmp/ws", 1, 1, []string{})
	if err == nil {
		t.Error("expected error for empty cmd")
	}
}

func TestRunLinux_BwrapFallback(t *testing.T) {
	requireLinux(t)

	available, availErr := BwrapAvailable()
	cmd, _, err := Run("/tmp/ws", 1, 1, []string{"true"})
	if err != nil {
		t.Fatalf("Run() returned error: %v", err)
	}
	if availErr != nil || !available {
		// bwrap unavailable — should fall back to plain exec
		if cmd.Path != "true" && filepath.Base(cmd.Path) != "true" {
			t.Errorf("expected plain exec fallback (true), got %s", cmd.Path)
		}
	} else {
		// bwrap IS available — must use sandbox
		bwrapPath, _ := bwrapLookup()
		if cmd.Path != bwrapPath {
			t.Errorf("expected bwrap sandbox (%s), got %s", bwrapPath, cmd.Path)
		}
	}
}

func TestRunLinux_NonExistentCmd(t *testing.T) {
	requireLinux(t)

	// Run returns *exec.Cmd without error — actual exec failure happens at cmd.Start()
	cmd, _, err := Run("/tmp/ws", 1, 1, []string{"nonexistent_command_xyz"})
	if err != nil {
		t.Fatalf("Run() should not error on nonexistent command: %v", err)
	}
	if cmd == nil {
		t.Fatal("expected non-nil *exec.Cmd")
	}
}

func TestRunLinux_CmdArgsArePassed(t *testing.T) {
	requireLinux(t)

	cmd, _, err := Run("/tmp/ws", 1, 1, []string{"echo", "-n", "test"})
	if err != nil {
		t.Fatalf("Run() returned error: %v", err)
	}
	// In plain exec fallback, args should be ["echo", "-n", "test"]
	if !isPlainExec(cmd) {
		t.Skip("bwrap sandbox active — args are wrapped, not directly comparable")
	}
	if len(cmd.Args) < 2 {
		t.Errorf("expected at least 2 args, got %d: %v", len(cmd.Args), cmd.Args)
	}
}

// isPlainExec checks if cmd looks like a plain os/exec command (not bwrap-wrapped).
func isPlainExec(cmd *exec.Cmd) bool {
	return !strings.Contains(cmd.Path, "pb-bwrap")
}

func TestBwrapBinary_VariableExists(t *testing.T) {
	// bwrapBinary is always defined (nil on unsupported platforms, populated on linux/386|amd64|arm|arm64)
	supported := runtime.GOOS == "linux" && (runtime.GOARCH == "386" || runtime.GOARCH == "amd64" || runtime.GOARCH == "arm" || runtime.GOARCH == "arm64")
	if supported {
		if len(bwrapBinary) == 0 {
			t.Log("bwrapBinary is empty — placeholder file present, compile real bwrap binary before deployment")
		}
	} else {
		if len(bwrapBinary) != 0 {
			t.Errorf("bwrapBinary should be nil on %s/%s, got %d bytes", runtime.GOOS, runtime.GOARCH, len(bwrapBinary))
		}
	}
}
