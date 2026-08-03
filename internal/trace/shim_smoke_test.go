package trace

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// TestTraceShimEscapesControlChars is the smoke test for
// fix-shim-json-control-char-loss: the shim must JSON-escape every control
// char (newline / tab / CR / quotes / backslash / other < 0x20) in step_id and
// summary so the emitted line parses cleanly via ParseLine — the old naive
// escape left newline/tab raw, producing invalid JSON that ParseLine silently
// skipped and dropping the agent's step event from the trace.
func TestTraceShimEscapesControlChars(t *testing.T) {
	shim := filepath.Join("..", "..", "examples", "trace-shim.sh")
	if _, err := os.Stat(shim); err != nil {
		t.Skipf("trace-shim.sh not found at %s: %v", shim, err)
	}
	trueBin, err := exec.LookPath("true")
	if err != nil {
		t.Skipf("true not in PATH: %v", err)
	}

	dir := t.TempDir()
	traceFile := filepath.Join(dir, "trace.jsonl")

	// A summary carrying every char the old escape left raw: newline, tab, CR,
	// a double-quote, and a backslash, plus a low control char (0x01).
	summary := "line one\n\tline two\r\nline three with \"quotes\" and a \\ backslash\x01end"

	cmd := exec.Command("bash", shim, "step-1", summary, "--", trueBin)
	cmd.Env = append(os.Environ(), "DRIFTLEDGER_TRACE="+traceFile)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("shim run failed: %v\n%s", err, out)
	}

	data, err := os.ReadFile(traceFile)
	if err != nil {
		t.Fatalf("read trace: %v", err)
	}
	lines := strings.Split(strings.TrimRight(string(data), "\n"), "\n")
	if len(lines) != 1 {
		t.Fatalf("expected 1 trace line, got %d: %q", len(lines), string(data))
	}
	ev, err := ParseLine(lines[0])
	if err != nil {
		t.Fatalf("ParseLine failed (control char not escaped?): %v\nline: %s", err, lines[0])
	}
	if ev.StepID != "step-1" {
		t.Errorf("step_id = %q, want step-1", ev.StepID)
	}
	if ev.Summary != summary {
		t.Errorf("summary round-trip mismatch:\n got: %q\nwant: %q", ev.Summary, summary)
	}
}

// TestTraceShimEscapesPlainSummary guards against the escape mangling a plain
// ASCII summary (no control chars) — the common case must pass through intact.
func TestTraceShimEscapesPlainSummary(t *testing.T) {
	shim := filepath.Join("..", "..", "examples", "trace-shim.sh")
	if _, err := os.Stat(shim); err != nil {
		t.Skipf("trace-shim.sh not found at %s: %v", shim, err)
	}
	trueBin, err := exec.LookPath("true")
	if err != nil {
		t.Skipf("true not in PATH: %v", err)
	}
	dir := t.TempDir()
	traceFile := filepath.Join(dir, "trace.jsonl")
	summary := "go mod init github.com/SuperMarioYL/driftledger"
	cmd := exec.Command("bash", shim, "step-1", summary, "--", trueBin)
	cmd.Env = append(os.Environ(), "DRIFTLEDGER_TRACE="+traceFile)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("shim run failed: %v\n%s", err, out)
	}
	data, err := os.ReadFile(traceFile)
	if err != nil {
		t.Fatalf("read trace: %v", err)
	}
	ev, err := ParseLine(strings.TrimRight(string(data), "\n"))
	if err != nil {
		t.Fatalf("ParseLine: %v", err)
	}
	if ev.Summary != summary {
		t.Errorf("plain summary mangled: got %q, want %q", ev.Summary, summary)
	}
}
