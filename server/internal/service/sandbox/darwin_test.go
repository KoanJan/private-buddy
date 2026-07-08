package sandbox

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func requireDarwin(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("sw_vers"); err != nil {
		t.Skip("test requires macOS")
	}
}

// sandboxExecRunnable returns true if sandbox-exec can apply a Seatbelt policy
// in the current environment. Uses a minimal valid policy for accurate detection:
// - "no version specified" (exit 65) means the policy file is invalid, not that the sandbox is unavailable
// - "sandbox_apply: Operation not permitted" (exit 71) means SIP blocked sandbox_apply
func sandboxExecRunnable() bool {
	// minimal valid Seatbelt policy — without (version 1) the error is "no version specified"
	const minimalPolicy = "(version 1)\n(allow default)\n"
	f, err := os.CreateTemp("", "pbsb-check-*.sb")
	if err != nil {
		return false
	}
	defer os.Remove(f.Name())
	if _, err := f.WriteString(minimalPolicy); err != nil {
		f.Close()
		return false
	}
	f.Close()
	_, err = exec.Command("/usr/bin/sandbox-exec", "-f", f.Name(), "--", "true").CombinedOutput()
	return err == nil
}

func TestGeneratePolicy_WorkspaceReplaced(t *testing.T) {
	requireDarwin(t)

	workspace := "/tmp/test-workspace"
	policy := generatePolicy(workspace)

	if strings.Contains(policy, "$WORKSPACE") {
		t.Error("policy still contains $WORKSPACE placeholder after generation")
	}
	if !strings.Contains(policy, workspace) {
		t.Errorf("policy does not contain workspace path %q", workspace)
	}
}

func TestRunDarwin_Echo(t *testing.T) {
	requireDarwin(t)
	if !sandboxExecRunnable() {
		t.Skip("sandbox-exec cannot apply policies in this environment (sandbox_apply denied)")
	}

	ws, err := os.MkdirTemp("", "pbtest-darwin-*")
	if err != nil {
		t.Fatalf("failed to create temp workspace: %v", err)
	}
	defer os.RemoveAll(ws)

	execCmd, _, err := runDarwin(ws, 1, 1, []string{"echo", "hello"})
	if err != nil {
		t.Fatalf("runDarwin() returned error: %v", err)
	}

	out, err := execCmd.Output()
	if err != nil {
		t.Fatalf("sandbox-exec of echo failed: %v", err)
	}
	if !strings.Contains(string(out), "hello") {
		t.Errorf("expected output to contain 'hello', got %q", string(out))
	}
}

func TestRunDarwin_WriteDenied(t *testing.T) {
	requireDarwin(t)
	if !sandboxExecRunnable() {
		t.Skip("sandbox-exec cannot apply policies in this environment (sandbox_apply denied)")
	}

	ws, err := os.MkdirTemp("", "pbtest-darwin-*")
	if err != nil {
		t.Fatalf("failed to create temp workspace: %v", err)
	}
	defer os.RemoveAll(ws)

	// Attempt to write to a protected directory
	execCmd, _, err := runDarwin(ws, 1, 1, []string{"touch", "/private/etc/sandbox_test_probe"})
	if err != nil {
		t.Fatalf("runDarwin() returned error: %v", err)
	}

	out, err := execCmd.CombinedOutput()
	if err == nil {
		// Success means write was NOT blocked — sandbox not active
		os.Remove("/private/etc/sandbox_test_probe")
		t.Error("write to /private/etc should have been denied by Seatbelt sandbox")
		return
	}
	combined := string(out)
	if !strings.Contains(combined, "deny") && !strings.Contains(combined, "Operation not permitted") {
		t.Errorf("expected Seatbelt deny message in output, got: %s", combined)
	}
}

func TestRunDarwin_WriteAllowed(t *testing.T) {
	requireDarwin(t)
	if !sandboxExecRunnable() {
		t.Skip("sandbox-exec cannot apply policies in this environment (sandbox_apply denied)")
	}

	ws, err := os.MkdirTemp("", "pbtest-darwin-*")
	if err != nil {
		t.Fatalf("failed to create temp workspace: %v", err)
	}
	defer os.RemoveAll(ws)

	testFile := filepath.Join(ws, "sandbox_write_test.txt")
	execCmd, _, err := runDarwin(ws, 1, 1, []string{"touch", testFile})
	if err != nil {
		t.Fatalf("runDarwin() returned error: %v", err)
	}

	if out, err := execCmd.CombinedOutput(); err != nil {
		t.Fatalf("touch workspace file failed: %v\n%s", err, string(out))
	}

	if _, statErr := os.Stat(testFile); os.IsNotExist(statErr) {
		t.Error("workspace file was not created — sandbox may be blocking legitimate writes")
	}
}

func TestRunDarwin_EmptyCmd(t *testing.T) {
	requireDarwin(t)

	_, _, err := Run("/tmp/ws", 1, 1, []string{})
	if err == nil {
		t.Error("expected error for empty cmd")
	}
}
