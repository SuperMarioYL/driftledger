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

// TestRollbackAppendsRollbackEntries (v0.3.0 impl-rollback-directive): Rollback
// appends one op:rollback LedgerEntry per deviation so the ledger records the
// loop close (closing the patch/accept/rollback loop alongside `driftledger rollback`).
func TestRollbackAppendsRollbackEntries(t *testing.T) {
	path := filepath.Join(t.TempDir(), "ledger.jsonl")
	l := New(path)
	drifting := dev("step-2", diff.KindDrifting)
	drifting.Summary = "punted on statuses"
	unex := dev("step-3", diff.KindUnexecuted)
	if err := l.Rollback("0.1.0", []diff.Deviation{drifting, unex}); err != nil {
		t.Fatalf("Rollback: %v", err)
	}
	entries, err := Read(path)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if len(entries) != 2 {
		t.Fatalf("entries = %d, want 2", len(entries))
	}
	for i, e := range entries {
		if e.Op != OpRollback {
			t.Errorf("entries[%d].op = %q, want rollback", i, e.Op)
		}
		if e.PlanVersion != "0.1.0" {
			t.Errorf("entries[%d].plan_version = %q, want 0.1.0", i, e.PlanVersion)
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

// TestRollbackNoDeviationsAppendsMarker (v0.3.0): with no deviations, Rollback
// still appends a single marker entry so the ledger records the rollback event.
func TestRollbackNoDeviationsAppendsMarker(t *testing.T) {
	path := filepath.Join(t.TempDir(), "ledger.jsonl")
	l := New(path)
	if err := l.Rollback("0.1.0", nil); err != nil {
		t.Fatalf("Rollback: %v", err)
	}
	entries, err := Read(path)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("entries = %d, want 1 marker entry", len(entries))
	}
	if entries[0].Op != OpRollback {
		t.Errorf("op = %q, want rollback", entries[0].Op)
	}
	if entries[0].PlanVersion != "0.1.0" {
		t.Errorf("plan_version = %q, want 0.1.0", entries[0].PlanVersion)
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

// TestAcceptedStepIDsResetsAfterPatch (v0.5.0 fix-accepted-overlay-leaks-past-patch):
// AcceptedStepIDs must reset the accepted set on an OpPatch entry (mirroring
// pendingAccepted) so only accepts recorded SINCE the most recent patch remain
// in the overlay. Without the reset, a step accepted pre-patch stays marked
// [accepted] after the patch — masking post-patch drift on the same step as
// already-accepted and blocking re-acceptance, breaking the
// accept→patch→accept loop that is the product's core workflow.
func TestAcceptedStepIDsResetsAfterPatch(t *testing.T) {
	path := filepath.Join(t.TempDir(), "ledger.jsonl")
	l := New(path)
	// 1. step-2 accepted pre-patch — this accept is folded by the patch.
	if err := l.Accept("0.1.0", dev("step-2", diff.KindDrifting)); err != nil {
		t.Fatalf("Accept: %v", err)
	}
	// 2. patch folds the accepted deviation — the accept is consumed.
	if err := l.Patch("0.1.1", []diff.Deviation{dev("step-2", diff.KindDrifting)}); err != nil {
		t.Fatalf("Patch: %v", err)
	}
	// 3. post-patch: the agent drifts on step-2 again. The overlay must NOT
	//    mark it accepted (the pre-patch accept was folded by the patch) so
	//    the user can press `a` to re-accept — the accept→patch→accept loop.
	accepted, err := AcceptedStepIDs(path)
	if err != nil {
		t.Fatalf("AcceptedStepIDs: %v", err)
	}
	if accepted["step-2"] {
		t.Errorf("step-2 still marked accepted after patch — overlay leaked past the patch (re-acceptance blocked)")
	}
	// 4. re-accept step-2 post-patch — the overlay must mark it accepted now.
	if err := l.Accept("0.1.1", dev("step-2", diff.KindDrifting)); err != nil {
		t.Fatalf("re-Accept: %v", err)
	}
	accepted, err = AcceptedStepIDs(path)
	if err != nil {
		t.Fatalf("AcceptedStepIDs after re-accept: %v", err)
	}
	if !accepted["step-2"] {
		t.Errorf("step-2 should be accepted after a post-patch re-accept — the accept→patch→accept loop is broken")
	}
}

// TestAcceptedStepIDsResetsAfterRollback covers the OpRollback branch of the
// overlay reset (v0.5.0 fix-accepted-overlay-leaks-past-patch): a rollback
// entry consumes the accepted set exactly like a patch entry, so a step
// accepted pre-rollback must not stay marked [accepted] afterwards.
func TestAcceptedStepIDsResetsAfterRollback(t *testing.T) {
	path := filepath.Join(t.TempDir(), "ledger.jsonl")
	l := New(path)
	if err := l.Accept("0.1.0", dev("step-2", diff.KindDrifting)); err != nil {
		t.Fatalf("Accept: %v", err)
	}
	if err := l.Rollback("0.1.0", []diff.Deviation{dev("step-2", diff.KindDrifting)}); err != nil {
		t.Fatalf("Rollback: %v", err)
	}
	accepted, err := AcceptedStepIDs(path)
	if err != nil {
		t.Fatalf("AcceptedStepIDs: %v", err)
	}
	if accepted["step-2"] {
		t.Errorf("step-2 still marked accepted after rollback — overlay leaked past the rollback")
	}
	// a fresh post-rollback accept re-enters the overlay.
	if err := l.Accept("0.1.0", dev("step-3", diff.KindDrifting)); err != nil {
		t.Fatalf("Accept step-3: %v", err)
	}
	accepted, err = AcceptedStepIDs(path)
	if err != nil {
		t.Fatalf("AcceptedStepIDs after accept: %v", err)
	}
	if !accepted["step-3"] {
		t.Errorf("step-3 should be accepted after a post-rollback accept")
	}
}
