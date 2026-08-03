package cmds

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/SuperMarioYL/driftledger/internal/diff"
	"github.com/SuperMarioYL/driftledger/internal/ledger"
	"github.com/SuperMarioYL/driftledger/internal/plan"
)

// TestRunPatchFoldsAcceptedDeviations is the end-to-end m2 acceptance test:
// `driftledger patch` rewrites plan.md to a bumped semantic version folding
// accepted deviations, and appends a `patch` LedgerEntry to the ledger.
func TestRunPatchFoldsAcceptedDeviations(t *testing.T) {
	dir := t.TempDir()
	planPath := filepath.Join(dir, "plan.md")
	ledgerPath := filepath.Join(dir, "ledger.jsonl")
	if err := os.WriteFile(planPath, []byte(plan.DefaultPlanMarkdown), 0o644); err != nil {
		t.Fatalf("write plan: %v", err)
	}
	// Seed the ledger with an accept for step-2 (drifting) — the deviation
	// the next patch should fold into the contract.
	l := ledger.New(ledgerPath)
	if err := l.Accept("0.1.0", diff.Deviation{
		StepID:  "step-2",
		Kind:    diff.KindDrifting,
		Summary: "punted on statuses",
	}); err != nil {
		t.Fatalf("Accept: %v", err)
	}

	var out bytes.Buffer
	if err := runPatch(planPath, ledgerPath, &out); err != nil {
		t.Fatalf("runPatch: %v", err)
	}
	if !bytes.Contains(out.Bytes(), []byte("v0.1.1")) {
		t.Errorf("output missing v0.1.1:\n%s", out.String())
	}
	if !bytes.Contains(out.Bytes(), []byte("folded")) {
		t.Errorf("output should report folded deviation count:\n%s", out.String())
	}

	// The plan was rewritten: version bumped to 0.1.1 and step-2 carries an
	// accepted annotation.
	p, err := plan.ParseFile(planPath)
	if err != nil {
		t.Fatalf("reparse patched plan: %v", err)
	}
	if p.Version != "0.1.1" {
		t.Errorf("patched plan version = %q, want 0.1.1", p.Version)
	}
	rewritten, err := os.ReadFile(planPath)
	if err != nil {
		t.Fatalf("read patched plan: %v", err)
	}
	if !bytes.Contains(rewritten, []byte("accepted: drifting folded into v0.1.1")) {
		t.Errorf("patched plan missing step-2 accepted annotation:\n%s", rewritten)
	}

	// The ledger now carries a patch entry stamping the new version.
	entries, err := ledger.Read(ledgerPath)
	if err != nil {
		t.Fatalf("Read ledger: %v", err)
	}
	var patch ledger.Entry
	var hasPatch bool
	for _, e := range entries {
		if e.Op == ledger.OpPatch {
			patch = e
			hasPatch = true
			break
		}
	}
	if !hasPatch {
		t.Fatal("no patch entry appended to ledger")
	}
	if patch.PlanVersion != "0.1.1" {
		t.Errorf("patch entry plan_version = %q, want 0.1.1", patch.PlanVersion)
	}
	if patch.Deviation.PatchedToVersion != "0.1.1" {
		t.Errorf("patch entry patched_to_version = %q, want 0.1.1", patch.Deviation.PatchedToVersion)
	}
	if patch.Deviation.StepID != "step-2" {
		t.Errorf("patch entry step_id = %q, want step-2", patch.Deviation.StepID)
	}
}

// TestRunPatchNoPendingJustBumpsVersion: with no accepted deviations, patch
// still bumps the plan version and appends a single marker patch entry so the
// ledger audit trail records the revision.
func TestRunPatchNoPendingJustBumpsVersion(t *testing.T) {
	dir := t.TempDir()
	planPath := filepath.Join(dir, "plan.md")
	ledgerPath := filepath.Join(dir, "ledger.jsonl")
	if err := os.WriteFile(planPath, []byte(plan.DefaultPlanMarkdown), 0o644); err != nil {
		t.Fatalf("write plan: %v", err)
	}
	var out bytes.Buffer
	if err := runPatch(planPath, ledgerPath, &out); err != nil {
		t.Fatalf("runPatch: %v", err)
	}
	p, _ := plan.ParseFile(planPath)
	if p.Version != "0.1.1" {
		t.Errorf("version = %q, want 0.1.1", p.Version)
	}
	entries, _ := ledger.Read(ledgerPath)
	if len(entries) != 1 || entries[0].Op != ledger.OpPatch {
		t.Errorf("expected 1 patch marker entry, got %v", entries)
	}
	if entries[0].PlanVersion != "0.1.1" {
		t.Errorf("marker plan_version = %q, want 0.1.1", entries[0].PlanVersion)
	}
}

// TestRunPatchIsIdempotentAcrossVersionBumps: a second patch (with no new
// accepts) bumps the version again rather than erroring, and does not leave
// stale pending state — the prior patch entry reset the pending set.
func TestRunPatchSecondCallBumpsAgain(t *testing.T) {
	dir := t.TempDir()
	planPath := filepath.Join(dir, "plan.md")
	ledgerPath := filepath.Join(dir, "ledger.jsonl")
	if err := os.WriteFile(planPath, []byte(plan.DefaultPlanMarkdown), 0o644); err != nil {
		t.Fatalf("write plan: %v", err)
	}
	var out bytes.Buffer
	if err := runPatch(planPath, ledgerPath, &out); err != nil {
		t.Fatalf("first runPatch: %v", err)
	}
	if err := runPatch(planPath, ledgerPath, &out); err != nil {
		t.Fatalf("second runPatch: %v", err)
	}
	p, _ := plan.ParseFile(planPath)
	if p.Version != "0.1.2" {
		t.Errorf("version after two patches = %q, want 0.1.2", p.Version)
	}
	// Two patch entries (one per call), no accepts folded in either.
	entries, _ := ledger.Read(ledgerPath)
	var patchCount int
	for _, e := range entries {
		if e.Op == ledger.OpPatch {
			patchCount++
		}
	}
	if patchCount != 2 {
		t.Errorf("patch entries = %d, want 2", patchCount)
	}
}

// TestPendingAcceptedResetsAfterPatch: accepts recorded before a patch entry
// are folded (reset); accepts recorded after the most recent patch remain
// pending for the next `patch` call. Latest accept per step wins.
func TestPendingAcceptedResetsAfterPatch(t *testing.T) {
	d := func(id string) diff.Deviation { return diff.Deviation{StepID: id, Kind: diff.KindDrifting} }
	entries := []ledger.Entry{
		{Op: ledger.OpAccept, Deviation: d("step-1")},
		{Op: ledger.OpAccept, Deviation: d("step-2")},
		{Op: ledger.OpPatch, PlanVersion: "0.1.1", Deviation: d("step-1")},
		{Op: ledger.OpAccept, Deviation: d("step-3")},
	}
	pending := pendingAccepted(entries)
	if len(pending) != 1 {
		t.Fatalf("pending = %d, want 1 (step-3 only after the patch reset)", len(pending))
	}
	if pending[0].StepID != "step-3" {
		t.Errorf("pending[0].step_id = %q, want step-3", pending[0].StepID)
	}
}

// TestPendingAcceptedLatestPerStepWins: when the same step is accepted twice
// between patches, the latest accept snapshot is the one folded.
func TestPendingAcceptedLatestPerStepWins(t *testing.T) {
	d := func(id string, summary string) diff.Deviation {
		return diff.Deviation{StepID: id, Kind: diff.KindDrifting, Summary: summary}
	}
	entries := []ledger.Entry{
		{Op: ledger.OpAccept, Deviation: d("step-2", "first")},
		{Op: ledger.OpAccept, Deviation: d("step-2", "second")},
	}
	pending := pendingAccepted(entries)
	if len(pending) != 1 {
		t.Fatalf("pending = %d, want 1", len(pending))
	}
	if pending[0].Summary != "second" {
		t.Errorf("pending summary = %q, want second (latest wins)", pending[0].Summary)
	}
}
