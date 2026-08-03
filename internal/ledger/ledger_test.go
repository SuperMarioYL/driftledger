package ledger

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/SuperMarioYL/driftledger/internal/diff"
)

func dev(id string, kind diff.Kind) diff.Deviation {
	return diff.Deviation{
		StepID:      id,
		Kind:        kind,
		FirstSeenTS: time.Date(2026, 7, 23, 10, 5, 0, 0, time.UTC),
	}
}

func TestAppendAndRead(t *testing.T) {
	path := filepath.Join(t.TempDir(), "ledger.jsonl")
	l := New(path)

	if err := l.Accept("0.1.0", dev("step-2", diff.KindDrifting)); err != nil {
		t.Fatalf("Accept: %v", err)
	}
	if err := l.Accept("0.1.0", dev("step-3", diff.KindUnexecuted)); err != nil {
		t.Fatalf("Accept: %v", err)
	}

	entries, err := Read(path)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if len(entries) != 2 {
		t.Fatalf("entries = %d, want 2", len(entries))
	}
	if entries[0].Op != OpAccept {
		t.Errorf("op = %q, want accept", entries[0].Op)
	}
	if entries[0].Deviation.StepID != "step-2" {
		t.Errorf("step_id = %q", entries[0].Deviation.StepID)
	}
	if entries[0].TS.IsZero() {
		t.Error("TS should be stamped now, not zero")
	}
}

func TestAppendConcurrency(t *testing.T) {
	path := filepath.Join(t.TempDir(), "ledger.jsonl")
	l := New(path)

	done := make(chan error, 8)
	for i := 0; i < 8; i++ {
		go func() {
			done <- l.Accept("0.1.0", dev("step-1", diff.KindDrifting))
		}()
	}
	for i := 0; i < 8; i++ {
		if err := <-done; err != nil {
			t.Fatalf("concurrent Accept: %v", err)
		}
	}
	entries, err := Read(path)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if len(entries) != 8 {
		t.Fatalf("entries = %d, want 8", len(entries))
	}
}

func TestReadMissingFile(t *testing.T) {
	entries, err := Read(filepath.Join(t.TempDir(), "absent.jsonl"))
	if err != nil {
		t.Fatalf("Read missing file: %v", err)
	}
	if entries != nil {
		t.Errorf("missing file should yield nil entries, got %d", len(entries))
	}
}

func TestReadSkipsMalformedLines(t *testing.T) {
	path := filepath.Join(t.TempDir(), "ledger.jsonl")
	good, _ := json.Marshal(Entry{Op: OpAccept, Deviation: dev("step-1", diff.KindDrifting)})
	content := append(good, '\n')
	content = append(content, []byte("not json\n")...)
	if err := os.WriteFile(path, content, 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	entries, err := Read(path)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if len(entries) != 1 {
		t.Errorf("entries = %d, want 1 (malformed line skipped)", len(entries))
	}
}

func TestAcceptedStepIDs(t *testing.T) {
	path := filepath.Join(t.TempDir(), "ledger.jsonl")
	l := New(path)
	if err := l.Accept("0.1.0", dev("step-2", diff.KindDrifting)); err != nil {
		t.Fatalf("Accept: %v", err)
	}
	if err := l.Accept("0.1.0", dev("step-3", diff.KindUnexecuted)); err != nil {
		t.Fatalf("Accept: %v", err)
	}

	accepted, err := AcceptedStepIDs(path)
	if err != nil {
		t.Fatalf("AcceptedStepIDs: %v", err)
	}
	if !accepted["step-2"] || !accepted["step-3"] {
		t.Errorf("accepted set = %v, want step-2 and step-3", accepted)
	}
	if accepted["step-1"] {
		t.Error("step-1 should not be accepted")
	}
}

func TestAcceptedStepIDsMissingFile(t *testing.T) {
	accepted, err := AcceptedStepIDs(filepath.Join(t.TempDir(), "absent.jsonl"))
	if err != nil {
		t.Fatalf("AcceptedStepIDs missing file: %v", err)
	}
	if len(accepted) != 0 {
		t.Errorf("missing file should yield empty accepted set, got %v", accepted)
	}
}

func TestPatchAppendsPatchEntries(t *testing.T) {
	path := filepath.Join(t.TempDir(), "ledger.jsonl")
	l := New(path)
	drifting := dev("step-2", diff.KindDrifting)
	drifting.Summary = "punted on statuses"
	unex := dev("step-3", diff.KindUnexecuted)
	if err := l.Patch("0.1.1", []diff.Deviation{drifting, unex}); err != nil {
		t.Fatalf("Patch: %v", err)
	}
	entries, err := Read(path)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if len(entries) != 2 {
		t.Fatalf("entries = %d, want 2", len(entries))
	}
	for i, e := range entries {
		if e.Op != OpPatch {
			t.Errorf("entries[%d].op = %q, want patch", i, e.Op)
		}
		if e.PlanVersion != "0.1.1" {
			t.Errorf("entries[%d].plan_version = %q, want 0.1.1", i, e.PlanVersion)
		}
		if e.Deviation.PatchedToVersion != "0.1.1" {
			t.Errorf("entries[%d].patched_to_version = %q, want 0.1.1", i, e.Deviation.PatchedToVersion)
		}
		if e.TS.IsZero() {
			t.Errorf("entries[%d].TS should be stamped now", i)
		}
	}
	if entries[0].Deviation.StepID != "step-2" {
		t.Errorf("entries[0].step_id = %q, want step-2", entries[0].Deviation.StepID)
	}
	if entries[1].Deviation.StepID != "step-3" {
		t.Errorf("entries[1].step_id = %q, want step-3", entries[1].Deviation.StepID)
	}
}

func TestPatchNoDeviationsAppendsMarker(t *testing.T) {
	path := filepath.Join(t.TempDir(), "ledger.jsonl")
	l := New(path)
	if err := l.Patch("0.1.1", nil); err != nil {
		t.Fatalf("Patch: %v", err)
	}
	entries, err := Read(path)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("entries = %d, want 1 marker entry", len(entries))
	}
	if entries[0].Op != OpPatch {
		t.Errorf("op = %q, want patch", entries[0].Op)
	}
	if entries[0].PlanVersion != "0.1.1" {
		t.Errorf("plan_version = %q, want 0.1.1", entries[0].PlanVersion)
	}
}

func TestEntryJSONShape(t *testing.T) {
	// The ledger line must stay `jq`-inspectable: top-level ts / plan_version /
	// op / deviation, with deviation.step_id present.
	e := Entry{
		TS:          time.Date(2026, 7, 23, 10, 5, 0, 0, time.UTC),
		PlanVersion: "0.1.0",
		Op:          OpAccept,
		Deviation:   dev("step-2", diff.KindDrifting),
	}
	b, err := json.Marshal(e)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var got map[string]any
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got["op"] != "accept" {
		t.Errorf("op = %v, want accept", got["op"])
	}
	if got["plan_version"] != "0.1.0" {
		t.Errorf("plan_version = %v", got["plan_version"])
	}
	dev, ok := got["deviation"].(map[string]any)
	if !ok {
		t.Fatal("deviation not an object")
	}
	if dev["step_id"] != "step-2" {
		t.Errorf("deviation.step_id = %v", dev["step_id"])
	}
}
