package tools

import (
	"testing"
)

// TestCycleDetect_NoCycle verifies that varying calls do not trigger detection.
func TestCycleDetect_NoCycle(t *testing.T) {
	d := &CycleDetector{}

	args1 := map[string]interface{}{"command": "ls"}
	args2 := map[string]interface{}{"command": "pwd"}

	// Different args, same result — not a cycle (input changed)
	cs := d.CycleDetect(args1, "output-a")
	if cs != NoCycleDetected {
		t.Fatalf("first call should not detect cycle, got %+v", cs)
	}

	cs = d.CycleDetect(args2, "output-a")
	if cs != NoCycleDetected {
		t.Fatalf("different args should not detect cycle, got %+v", cs)
	}
}

// TestCycleDetect_SameArgsDifferentResult verifies that when the same args
// produce a different result, the counter resets — this is progress, not a cycle.
func TestCycleDetect_SameArgsDifferentResult(t *testing.T) {
	d := &CycleDetector{}

	args := map[string]interface{}{"command": "date"}

	// Call 1: same args, result A
	cs := d.CycleDetect(args, "result-A")
	if cs != NoCycleDetected {
		t.Fatalf("first call should not detect, got %+v", cs)
	}

	// Call 2: same args, result A again → count=1, no warning yet
	cs = d.CycleDetect(args, "result-A")
	if cs != NoCycleDetected {
		t.Fatalf("count=1 should not warn, got %+v", cs)
	}

	// Call 3: same args, but different result → counter resets
	cs = d.CycleDetect(args, "result-B")
	if cs != NoCycleDetected {
		t.Fatalf("different result should reset counter, got %+v", cs)
	}

	// Call 4: same args, result B → count=0 (just reset), not 3
	cs = d.CycleDetect(args, "result-B")
	if cs != NoCycleDetected {
		t.Fatalf("after reset, count=1 should not warn, got %+v", cs)
	}
}

// TestCycleDetect_WarnThreshold verifies that the warning fires exactly at
// warnThreshold (3) consecutive identical calls.
func TestCycleDetect_WarnThreshold(t *testing.T) {
	d := &CycleDetector{}

	args := map[string]interface{}{"command": "echo hello"}
	result := "hello"

	// Call 1: count becomes 0 (first call, no previous to match)
	// Actually, first call: sig != "" (lastSignature default), so count=0
	cs := d.CycleDetect(args, result)
	if cs != NoCycleDetected {
		t.Fatalf("call 1: expected no detection, got %+v", cs)
	}

	// Call 2: same pair, count=1
	cs = d.CycleDetect(args, result)
	if cs != NoCycleDetected {
		t.Fatalf("call 2: expected no detection, got %+v", cs)
	}

	// Call 3: same pair, count=2
	cs = d.CycleDetect(args, result)
	if cs != NoCycleDetected {
		t.Fatalf("call 3: expected no detection (count=2 < threshold 3), got %+v", cs)
	}

	// Call 4: same pair, count=3 → warn
	cs = d.CycleDetect(args, result)
	if cs.Warning == "" {
		t.Fatal("call 4: expected warning at count=3")
	}
	if cs.Blocked {
		t.Fatal("call 4: should not be blocked at count=3")
	}

	// Call 5: same pair, count=4 → still warn
	cs = d.CycleDetect(args, result)
	if cs.Warning == "" {
		t.Fatal("call 5: expected warning at count=4")
	}
	if cs.Blocked {
		t.Fatal("call 5: should not be blocked at count=4")
	}
}

// TestCycleDetect_BlockThreshold verifies that block fires at blockThreshold (8).
// Note: the first call initializes lastSignature with count=0 (no previous to match).
// So count=N requires N+1 total calls. Block at count=8 needs 9 calls.
func TestCycleDetect_BlockThreshold(t *testing.T) {
	d := &CycleDetector{}

	args := map[string]interface{}{"command": "npm run dev"}
	result := `{"exit_code":1,"stderr":"Error: Cannot find module"}`

	// Call 1: count=0 (init), no detection
	// Calls 2-3: count=1→2, no detection
	for i := 0; i < 3; i++ {
		cs := d.CycleDetect(args, result)
		if cs != NoCycleDetected {
			t.Fatalf("call %d: expected no detection, got %+v", i+1, cs)
		}
	}

	// Calls 4-6: count=3→5, warnings
	for i := 0; i < 3; i++ {
		cs := d.CycleDetect(args, result)
		if cs.Warning == "" {
			t.Fatalf("call %d: expected warning, got %+v", i+4, cs)
		}
		if cs.Blocked {
			t.Fatalf("call %d: should not be blocked", i+4)
		}
	}

	// Calls 7-8: count=6→7, still warning only
	cs := d.CycleDetect(args, result)
	if cs.Warning == "" || cs.Blocked {
		t.Fatalf("call 7: expected warning only, got %+v", cs)
	}
	cs = d.CycleDetect(args, result)
	if cs.Warning == "" || cs.Blocked {
		t.Fatalf("call 8: expected warning only, got %+v", cs)
	}

	// Call 9: count=8 → block
	cs = d.CycleDetect(args, result)
	if !cs.Blocked {
		t.Fatal("call 9: expected block at count=8")
	}
	if cs.Reason == "" {
		t.Fatal("call 9: block reason should not be empty")
	}
}

// TestCycleDetect_BlockIsPersistent verifies that once blocked, the detector
// always returns blocked regardless of subsequent input.
func TestCycleDetect_BlockIsPersistent(t *testing.T) {
	d := &CycleDetector{}

	args := map[string]interface{}{"command": "ls"}
	result := "file1 file2"

	// Trigger block: first call initializes (count=0), then 8 more to reach count=8
	for i := 0; i < 9; i++ {
		d.CycleDetect(args, result)
	}

	// Now blocked — any call should return blocked
	cs := d.CycleDetect(map[string]interface{}{"command": "pwd"}, "different result")
	if !cs.Blocked {
		t.Fatal("after block, different args should still return blocked")
	}

	cs = d.CycleDetect(args, result)
	if !cs.Blocked {
		t.Fatal("after block, same args should still return blocked")
	}
}

// TestCycleDetect_CounterResetsOnDifferentArgs verifies that changing args
// resets the consecutive counter.
func TestCycleDetect_CounterResetsOnDifferentArgs(t *testing.T) {
	d := &CycleDetector{}

	args1 := map[string]interface{}{"command": "echo a"}
	args2 := map[string]interface{}{"command": "echo b"}
	result := "same output"

	// Build up to count=2 with args1
	d.CycleDetect(args1, result)
	d.CycleDetect(args1, result)

	// Different args → counter resets
	cs := d.CycleDetect(args2, result)
	if cs != NoCycleDetected {
		t.Fatalf("different args should reset, got %+v", cs)
	}

	// Back to args1: count should be 0, not 3
	d.CycleDetect(args1, result)
	cs = d.CycleDetect(args1, result)
	if cs != NoCycleDetected {
		t.Fatalf("after reset, count should be 1 not 3, got %+v", cs)
	}
}

// TestCycleDetect_NilArgs verifies that nil args don't panic.
func TestCycleDetect_NilArgs(t *testing.T) {
	d := &CycleDetector{}

	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("CycleDetect panicked on nil args: %v", r)
		}
	}()

	cs := d.CycleDetect(nil, "result")
	if cs != NoCycleDetected {
		t.Fatalf("first call with nil args should not detect, got %+v", cs)
	}

	// Same nil args + same result → should count
	cs = d.CycleDetect(nil, "result")
	if cs != NoCycleDetected {
		t.Fatalf("count=1 should not warn, got %+v", cs)
	}
}

// TestCycleDetect_EmptyResult verifies that empty result strings work correctly.
func TestCycleDetect_EmptyResult(t *testing.T) {
	d := &CycleDetector{}

	args := map[string]interface{}{"command": "true"}

	// Empty result is a valid output — same empty result should count.
	// First call initializes (count=0), then 7 more to reach count=7 (still warning).
	for i := 0; i < 8; i++ {
		cs := d.CycleDetect(args, "")
		if cs.Blocked {
			t.Fatalf("call %d: should not block before threshold", i+1)
		}
	}

	// 9th call → count=8 → block
	cs := d.CycleDetect(args, "")
	if !cs.Blocked {
		t.Fatal("9th call with identical empty result should block")
	}
}

// TestCycleDetect_ArgsOrderInvariant verifies that the same args with different
// map key order produce the same signature (JSON marshalling sorts keys).
func TestCycleDetect_ArgsOrderInvariant(t *testing.T) {
	d := &CycleDetector{}

	// Go maps don't guarantee iteration order, but json.Marshal sorts keys.
	// So two maps with the same content will produce the same signature.
	args1 := map[string]interface{}{"a": "1", "b": "2"}
	args2 := map[string]interface{}{"b": "2", "a": "1"}
	result := "output"

	// Call 1 with args1
	d.CycleDetect(args1, result)

	// Call 2 with args2 (same content, different declaration order)
	// Should be treated as same args → count increments
	cs := d.CycleDetect(args2, result)
	if cs != NoCycleDetected {
		// count=1, not enough for warning
		if cs.Warning != "" {
			t.Fatalf("count=1 should not warn, got %+v", cs)
		}
	}

	// Call 3 with args1 again → count=2, still no warning
	cs = d.CycleDetect(args1, result)
	if cs.Warning != "" {
		t.Fatalf("count=2 should not warn, got %+v", cs)
	}

	// Call 4 → count=3, warning
	cs = d.CycleDetect(args2, result)
	if cs.Warning == "" {
		t.Fatal("count=3 should warn — args order should not matter")
	}
}
