package trace

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestParseLine(t *testing.T) {
	ev, err := ParseLine(`{"ts":"2026-07-23T10:00:00Z","step_id":"step-1","action":"run","summary":"go mod init"}`)
	if err != nil {
		t.Fatalf("ParseLine: %v", err)
	}
	want := time.Date(2026, 7, 23, 10, 0, 0, 0, time.UTC)
	if !ev.TS.Equal(want) {
		t.Errorf("ts = %v, want %v", ev.TS, want)
	}
	if ev.StepID != "step-1" {
		t.Errorf("step_id = %q", ev.StepID)
	}
	if ev.Action != "run" {
		t.Errorf("action = %q", ev.Action)
	}
	if ev.Summary != "go mod init" {
		t.Errorf("summary = %q", ev.Summary)
	}
}

func TestParseLineOptionalStepID(t *testing.T) {
	ev, err := ParseLine(`{"action":"note","summary:"exploring the codebase"}`)
	// ^ that line is malformed JSON (summary: not summary)
	if err == nil {
		t.Fatalf("expected parse error for malformed line, got %+v", ev)
	}
}

func TestParseLineMissingAction(t *testing.T) {
	if _, err := ParseLine(`{"summary":"no action field"}`); err == nil {
		t.Fatal("expected error for event missing action, got nil")
	}
}

func TestParseLineBlankEOF(t *testing.T) {
	if _, err := ParseLine(""); err == nil {
		t.Fatal("expected io.EOF for blank line, got nil")
	}
	if _, err := ParseLine("   \n"); err == nil {
		t.Fatal("expected io.EOF for whitespace-only line, got nil")
	}
}

func TestParseLineDefaultsTS(t *testing.T) {
	ev, err := ParseLine(`{"action":"run","summary":"no ts field"}`)
	if err != nil {
		t.Fatalf("ParseLine: %v", err)
	}
	if ev.TS.IsZero() {
		t.Error("missing ts should default to now, not stay zero")
	}
}

func TestParseReader(t *testing.T) {
	input := strings.Join([]string{
		`{"ts":"2026-07-23T10:00:00Z","step_id":"step-1","action":"run","summary":"scaffold module"}`,
		`{"ts":"2026-07-23T10:01:00Z","action":"note","summary":"exploring"}`,
		`not json at all`,
		`{"ts":"2026-07-23T10:02:00Z","step_id":"step-3","action":"run","summary":"writing tests"}`,
		"",
	}, "\n")
	events, skipped, _, err := ParseReader(strings.NewReader(input))
	if err != nil {
		t.Fatalf("ParseReader: %v", err)
	}
	if len(events) != 3 {
		t.Fatalf("events = %d, want 3", len(events))
	}
	if skipped != 1 {
		t.Errorf("skipped = %d, want 1", skipped)
	}
	if events[1].StepID != "" {
		t.Errorf("second event step_id should be empty, got %q", events[1].StepID)
	}
}

func TestParseFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "trace.jsonl")
	content := `{"ts":"2026-07-23T10:00:00Z","step_id":"step-1","action":"run","summary":"scaffold module"}
{"ts":"2026-07-23T10:01:00Z","step_id":"step-2","action":"run","summary":"reconciler matched status"}`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	events, _, _, err := ParseFile(path)
	if err != nil {
		t.Fatalf("ParseFile: %v", err)
	}
	if len(events) != 2 {
		t.Fatalf("events = %d, want 2", len(events))
	}
}

func TestParseFileMissing(t *testing.T) {
	if _, _, _, err := ParseFile("/nonexistent/trace.jsonl"); err == nil {
		t.Fatal("expected error for missing file, got nil")
	}
}

// TestParseFileSkippedCount (v0.3.0 fix-trace-parsefile-silent-skip) asserts that
// ParseFile returns the count of malformed lines so a caller can surface "N
// unparseable line(s) skipped" instead of silently reconciling a partial trace.
func TestParseFileSkippedCount(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "trace.jsonl")
	content := `{"ts":"2026-07-23T10:00:00Z","step_id":"step-1","action":"run","summary":"valid line"}
this is not json
{"ts":"2026-07-23T10:01:00Z","step_id":"step-2","action":"run","summary":"also valid"}
{broken
{"ts":"2026-07-23T10:02:00Z","step_id":"step-3","action":"run","summary":"third valid"}`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	events, skipped, _, err := ParseFile(path)
	if err != nil {
		t.Fatalf("ParseFile: %v", err)
	}
	if len(events) != 3 {
		t.Fatalf("events = %d, want 3 valid", len(events))
	}
	if skipped != 2 {
		t.Fatalf("skipped = %d, want 2 malformed", skipped)
	}
}

// TestParseReaderOutOfOrder (v0.4.0 feat-trace-out-of-order-ts) asserts that an
// event whose ts precedes the immediately-prior event's ts is counted as
// out-of-order, while first-seen ranking in diff.Reconcile still uses the
// earliest ts.
func TestParseReaderOutOfOrder(t *testing.T) {
	input := strings.Join([]string{
		`{"ts":"2026-07-23T10:00:00Z","step_id":"step-1","action":"run","summary":"first"}`,
		`{"ts":"2026-07-23T10:05:00Z","step_id":"step-2","action":"run","summary":"second"}`,
		`{"ts":"2026-07-23T10:02:00Z","step_id":"step-3","action":"run","summary":"skewed back"}`,
		`{"ts":"2026-07-23T10:06:00Z","step_id":"step-4","action":"run","summary":"fourth"}`,
	}, "\n")
	events, skipped, outOfOrder, err := ParseReader(strings.NewReader(input))
	if err != nil {
		t.Fatalf("ParseReader: %v", err)
	}
	if len(events) != 4 {
		t.Fatalf("events = %d, want 4", len(events))
	}
	if skipped != 0 {
		t.Errorf("skipped = %d, want 0", skipped)
	}
	if outOfOrder != 1 {
		t.Errorf("outOfOrder = %d, want 1 (the 10:02 event precedes the prior 10:05)", outOfOrder)
	}
	// first-seen for step-3 is its own ts (10:02), the earliest among its events.
	want := time.Date(2026, 7, 23, 10, 2, 0, 0, time.UTC)
	if !events[2].TS.Equal(want) {
		t.Errorf("step-3 ts = %v, want %v", events[2].TS, want)
	}
}
