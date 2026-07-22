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
	events, skipped, err := ParseReader(strings.NewReader(input))
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
	events, err := ParseFile(path)
	if err != nil {
		t.Fatalf("ParseFile: %v", err)
	}
	if len(events) != 2 {
		t.Fatalf("events = %d, want 2", len(events))
	}
}

func TestParseFileMissing(t *testing.T) {
	if _, err := ParseFile("/nonexistent/trace.jsonl"); err == nil {
		t.Fatal("expected error for missing file, got nil")
	}
}
