package cmds

import (
	"bytes"
	"encoding/json"
	"errors"
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

// TestPendingAcceptedResetsAfterRollback (v0.3.0 impl-rollback-directive): an
// OpRollback entry resets the pending set exactly like OpPatch, so accepts
// recorded before a rollback are consumed and only post-rollback accepts
// remain pending.
func TestPendingAcceptedResetsAfterRollback(t *testing.T) {
	d := func(id string) diff.Deviation { return diff.Deviation{StepID: id, Kind: diff.KindDrifting} }
	entries := []ledger.Entry{
		{Op: ledger.OpAccept, Deviation: d("step-1")},
		{Op: ledger.OpAccept, Deviation: d("step-2")},
		{Op: ledger.OpRollback, PlanVersion: "0.1.0", Deviation: d("step-1")},
		{Op: ledger.OpAccept, Deviation: d("step-3")},
	}
	pending := pendingAccepted(entries)
	if len(pending) != 1 {
		t.Fatalf("pending = %d, want 1 (step-3 only after the rollback reset)", len(pending))
	}
	if pending[0].StepID != "step-3" {
		t.Errorf("pending[0].step_id = %q, want step-3", pending[0].StepID)
	}
}

// TestRunRollbackEmitsDirectivesAndAppendsEntries (v0.3.0 impl-rollback-directive):
// `driftledger rollback` emits a git-revert + checkpoint-tag DIRECTIVE per
// accepted deviation to stdout (never executes it), and appends a rollback
// LedgerEntry per deviation so the ledger records the loop close.
func TestRunRollbackEmitsDirectivesAndAppendsEntries(t *testing.T) {
	dir := t.TempDir()
	planPath := filepath.Join(dir, "plan.md")
	ledgerPath := filepath.Join(dir, "ledger.jsonl")
	if err := os.WriteFile(planPath, []byte(plan.DefaultPlanMarkdown), 0o644); err != nil {
		t.Fatalf("write plan: %v", err)
	}
	l := ledger.New(ledgerPath)
	if err := l.Accept("0.1.0", diff.Deviation{
		StepID:  "step-2",
		Kind:    diff.KindDrifting,
		Summary: "punted on statuses",
	}); err != nil {
		t.Fatalf("Accept: %v", err)
	}

	var out bytes.Buffer
	if err := runRollback(planPath, ledgerPath, &out); err != nil {
		t.Fatalf("runRollback: %v", err)
	}
	// The directive is emitted to stdout and never auto-executed.
	if !bytes.Contains(out.Bytes(), []byte("git revert")) {
		t.Errorf("output missing git revert directive:\n%s", out.String())
	}
	if !bytes.Contains(out.Bytes(), []byte("driftledger-rollback-step-2")) {
		t.Errorf("output missing checkpoint tag:\n%s", out.String())
	}
	if !bytes.Contains(out.Bytes(), []byte("out of scope")) {
		t.Errorf("output should note auto-execution is out of scope:\n%s", out.String())
	}

	// A rollback LedgerEntry was appended.
	entries, err := ledger.Read(ledgerPath)
	if err != nil {
		t.Fatalf("Read ledger: %v", err)
	}
	var rb ledger.Entry
	var hasRollback bool
	for _, e := range entries {
		if e.Op == ledger.OpRollback {
			rb = e
			hasRollback = true
			break
		}
	}
	if !hasRollback {
		t.Fatal("no rollback entry appended to ledger")
	}
	if rb.Deviation.StepID != "step-2" {
		t.Errorf("rollback entry step_id = %q, want step-2", rb.Deviation.StepID)
	}
}

// TestRunRollbackNoPendingRecordsMarker (v0.3.0): with no pending accepts,
// rollback still appends a single marker entry so the ledger records the event.
func TestRunRollbackNoPendingRecordsMarker(t *testing.T) {
	dir := t.TempDir()
	planPath := filepath.Join(dir, "plan.md")
	ledgerPath := filepath.Join(dir, "ledger.jsonl")
	if err := os.WriteFile(planPath, []byte(plan.DefaultPlanMarkdown), 0o644); err != nil {
		t.Fatalf("write plan: %v", err)
	}
	var out bytes.Buffer
	if err := runRollback(planPath, ledgerPath, &out); err != nil {
		t.Fatalf("runRollback: %v", err)
	}
	entries, _ := ledger.Read(ledgerPath)
	if len(entries) != 1 || entries[0].Op != ledger.OpRollback {
		t.Errorf("expected 1 rollback marker entry, got %v", entries)
	}
}

// TestRunPatchAtomicOnLedgerFailure (v0.3.0 fix-patch-plan-ledger-desync): when
// the ledger append fails, the plan file is left byte-unchanged (the temp file
// is discarded), so a subsequent patch does not double-fold.
func TestRunPatchAtomicOnLedgerFailure(t *testing.T) {
	dir := t.TempDir()
	planPath := filepath.Join(dir, "plan.md")
	ledgerPath := filepath.Join(dir, "ledger.jsonl")
	origPlan := []byte(plan.DefaultPlanMarkdown)
	if err := os.WriteFile(planPath, origPlan, 0o644); err != nil {
		t.Fatalf("write plan: %v", err)
	}
	// Seed an accept so patch has something to fold.
	l := ledger.New(ledgerPath)
	if err := l.Accept("0.1.0", diff.Deviation{
		StepID:  "step-2",
		Kind:    diff.KindDrifting,
		Summary: "drift",
	}); err != nil {
		t.Fatalf("Accept: %v", err)
	}
	// Make the ledger file read-only so the patch ledger-append (O_WRONLY)
	// fails with EACCES — simulating a ledger-write failure.
	if err := os.Chmod(ledgerPath, 0o444); err != nil {
		t.Fatalf("chmod ledger: %v", err)
	}
	defer os.Chmod(ledgerPath, 0o644) // restore so TempDir cleanup works

	var out bytes.Buffer
	err := runPatch(planPath, ledgerPath, &out)
	if err == nil {
		t.Fatal("expected runPatch to fail when the ledger append fails")
	}
	// The plan file MUST be unchanged (temp file discarded, no rename).
	after, err := os.ReadFile(planPath)
	if err != nil {
		t.Fatalf("read plan after failed patch: %v", err)
	}
	if !bytes.Equal(after, origPlan) {
		t.Errorf("plan file changed despite ledger failure (desync!):\n--- want ---\n%s\n--- got ---\n%s", origPlan, after)
	}
}

// TestRunDiffJSON (v0.4.0 feat-diff-json-output): `driftledger diff --json`
// emits a JSON array of deviations that round-trips into []diff.Deviation so
// CI / another agent can consume drift state programmatically.
func TestRunDiffJSON(t *testing.T) {
	dir := t.TempDir()
	planPath := filepath.Join(dir, "plan.md")
	tracePath := filepath.Join(dir, "trace.jsonl")
	ledgerPath := filepath.Join(dir, "ledger.jsonl")
	if err := os.WriteFile(planPath, []byte(plan.DefaultPlanMarkdown), 0o644); err != nil {
		t.Fatalf("write plan: %v", err)
	}
	trace := `{"ts":"2026-07-23T10:00:00Z","step_id":"step-1","action":"run","summary":"go module initialized and cmd package present"}
{"ts":"2026-07-23T10:01:00Z","action":"note","summary":"exploring, no step id"}`
	if err := os.WriteFile(tracePath, []byte(trace), 0o644); err != nil {
		t.Fatalf("write trace: %v", err)
	}
	var out bytes.Buffer
	if err := runDiff(planPath, tracePath, ledgerPath, true, false, false, &out); err != nil {
		t.Fatalf("runDiff --json: %v", err)
	}
	var devs []diff.Deviation
	if err := json.Unmarshal(out.Bytes(), &devs); err != nil {
		t.Fatalf("--json output is not valid JSON / does not round-trip into []Deviation: %v\noutput:\n%s", err, out.String())
	}
	// 3 plan steps → 3 deviations (step-1 matched, step-2/3 unexecuted) + 1 extra
	// = 4. The exact kinds are reconciler-owned; here we assert the contract:
	// valid JSON, round-trips, and the step-1 deviation is present + matched.
	if len(devs) < 3 {
		t.Fatalf("deviations = %d, want >= 3 (one per plan step)", len(devs))
	}
	var step1 *diff.Deviation
	for i := range devs {
		if devs[i].StepID == "step-1" {
			step1 = &devs[i]
		}
	}
	if step1 == nil {
		t.Fatal("step-1 deviation missing from --json output")
	}
	if step1.Kind != diff.KindMatched {
		t.Errorf("step-1 kind = %q, want matched (its accept criteria appear in the trace)", step1.Kind)
	}
}

// TestRunPatchVPrefixedNoDoubleV (v0.4.0 fix-patch-version-double-v): a plan
// whose version is v-prefixed (e.g. "v0.1.0", the common semver convention)
// patches to a SINGLE "v0.1.1" in both stdout and the accepted annotation —
// never "vv0.1.1".
func TestRunPatchVPrefixedNoDoubleV(t *testing.T) {
	dir := t.TempDir()
	planPath := filepath.Join(dir, "plan.md")
	ledgerPath := filepath.Join(dir, "ledger.jsonl")
	planMD := `# Plan: demo

version: v0.1.0

## step-1
intent: Scaffold
accept: go module

## step-2
intent: Reconciler
accept: matched
`
	if err := os.WriteFile(planPath, []byte(planMD), 0o644); err != nil {
		t.Fatalf("write plan: %v", err)
	}
	l := ledger.New(ledgerPath)
	if err := l.Accept("v0.1.0", diff.Deviation{
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
		t.Errorf("stdout missing v0.1.1:\n%s", out.String())
	}
	if bytes.Contains(out.Bytes(), []byte("vv0.1.1")) {
		t.Errorf("stdout has DOUBLE-v (vv0.1.1) — the v0.4.0 fix regressed:\n%s", out.String())
	}
	rewritten, err := os.ReadFile(planPath)
	if err != nil {
		t.Fatalf("read patched plan: %v", err)
	}
	if bytes.Contains(rewritten, []byte("vv0.1.1")) {
		t.Errorf("patched plan has DOUBLE-v annotation (vv0.1.1):\n%s", rewritten)
	}
	if !bytes.Contains(rewritten, []byte("folded into v0.1.1")) {
		t.Errorf("patched plan missing single-v accepted annotation 'folded into v0.1.1':\n%s", rewritten)
	}
}

// TestRunPatchRollsBackLedgerOnRenameFailure (v0.5.0
// fix-patch-rename-failure-desync): when the atomic rename (temp -> plan)
// fails AFTER l.Patch has appended the patch entries, the just-appended
// entries MUST be rolled back (os.Truncate to the pre-patch size) so the
// ledger does not record a version the plan never reached — the desync the
// v0.3.0 atomicity fix closed for the l.Patch failure path. The rename
// failure is forced via the renameFn seam: CreateTemp and Rename share the
// same directory write permission, so a portable same-directory rename
// failure is otherwise unforceable from outside the function.
func TestRunPatchRollsBackLedgerOnRenameFailure(t *testing.T) {
	dir := t.TempDir()
	planPath := filepath.Join(dir, "plan.md")
	ledgerPath := filepath.Join(dir, "ledger.jsonl")
	origPlan := []byte(plan.DefaultPlanMarkdown)
	if err := os.WriteFile(planPath, origPlan, 0o644); err != nil {
		t.Fatalf("write plan: %v", err)
	}
	// Seed an accept so the patch has a deviation to fold AND the ledger
	// pre-exists with content (prePatchSize >= 0).
	l := ledger.New(ledgerPath)
	if err := l.Accept("0.1.0", diff.Deviation{
		StepID:  "step-2",
		Kind:    diff.KindDrifting,
		Summary: "punted on statuses",
	}); err != nil {
		t.Fatalf("Accept: %v", err)
	}
	prePatchLedger, err := os.ReadFile(ledgerPath)
	if err != nil {
		t.Fatalf("read ledger pre-patch: %v", err)
	}

	// Force os.Rename to fail so the just-appended patch entries must roll back.
	origRename := renameFn
	renameFn = func(string, string) error { return errors.New("simulated rename failure") }
	defer func() { renameFn = origRename }()

	var out bytes.Buffer
	err = runPatch(planPath, ledgerPath, &out)
	if err == nil {
		t.Fatal("expected runPatch to fail when rename fails, got nil")
	}
	if !bytes.Contains([]byte(err.Error()), []byte("rename")) {
		t.Errorf("error should mention rename: %v", err)
	}

	// The ledger MUST be byte-identical to its pre-patch state (patch entries
	// rolled back). The bug left the patch entries in place, desyncing the
	// audit trail with a patch entry for a version the plan never reached.
	afterLedger, err := os.ReadFile(ledgerPath)
	if err != nil {
		t.Fatalf("read ledger post-patch: %v", err)
	}
	if !bytes.Equal(prePatchLedger, afterLedger) {
		t.Errorf("ledger not rolled back to pre-patch state after rename failure (desync!):\n--- want ---\n%s\n--- got ---\n%s", prePatchLedger, afterLedger)
	}
	// The plan file must also be unchanged (rename failed; temp discarded).
	planAfter, err := os.ReadFile(planPath)
	if err != nil {
		t.Fatalf("read plan post-patch: %v", err)
	}
	if !bytes.Equal(origPlan, planAfter) {
		t.Errorf("plan changed despite rename failure (desync!):\n--- want ---\n%s\n--- got ---\n%s", origPlan, planAfter)
	}
}

// TestRunDiffFailOnDrift (v0.5.0 feat-diff-exit-code-on-drift): a plan+trace
// with an unaccepted drifting step must return a non-nil error under
// --fail-on-drift (CI exit 1) and nil without it. step-2 ran but its accept
// criterion is unsatisfied → drifting; step-1 matched.
func TestRunDiffFailOnDrift(t *testing.T) {
	dir := t.TempDir()
	planPath := filepath.Join(dir, "plan.md")
	tracePath := filepath.Join(dir, "trace.jsonl")
	ledgerPath := filepath.Join(dir, "ledger.jsonl")
	planMD := `# Plan: demo
version: 0.1.0
## step-1
intent: do thing
accept: done
## step-2
intent: do other
accept: finished
`
	if err := os.WriteFile(planPath, []byte(planMD), 0o644); err != nil {
		t.Fatalf("write plan: %v", err)
	}
	// step-1 matched (criterion "done" present); step-2 drifting (criterion
	// "finished" absent from its summary).
	traceContent := `{"ts":"2026-07-23T10:00:00Z","step_id":"step-1","action":"run","summary":"work done"}
{"ts":"2026-07-23T10:20:00Z","step_id":"step-2","action":"run","summary":"punted on the work"}
`
	if err := os.WriteFile(tracePath, []byte(traceContent), 0o644); err != nil {
		t.Fatalf("write trace: %v", err)
	}

	// With --fail-on-drift: the unaccepted drifting step-2 (plus unexecuted
	// is not present here, both steps ran) trips the gate → non-nil error.
	var out bytes.Buffer
	if err := runDiff(planPath, tracePath, ledgerPath, false, false, true, &out); err == nil {
		t.Fatal("expected drift error with --fail-on-drift, got nil")
	}
	// Without --fail-on-drift: same trace, no error (existing prose behaviour).
	out.Reset()
	if err := runDiff(planPath, tracePath, ledgerPath, false, false, false, &out); err != nil {
		t.Errorf("expected nil without --fail-on-drift, got %v", err)
	}
}

// TestRunDiffFailOnDriftAllMatched (v0.5.0 feat-diff-exit-code-on-drift): an
// all-matched trace exits 0 even under --fail-on-drift (matched steps do not
// trip the gate).
func TestRunDiffFailOnDriftAllMatched(t *testing.T) {
	dir := t.TempDir()
	planPath := filepath.Join(dir, "plan.md")
	tracePath := filepath.Join(dir, "trace.jsonl")
	ledgerPath := filepath.Join(dir, "ledger.jsonl")
	planMD := `# Plan: demo
version: 0.1.0
## step-1
intent: do thing
accept: done
## step-2
intent: do other
accept: finished
`
	if err := os.WriteFile(planPath, []byte(planMD), 0o644); err != nil {
		t.Fatalf("write plan: %v", err)
	}
	// Both steps satisfy their accept criteria → all matched.
	traceContent := `{"ts":"2026-07-23T10:00:00Z","step_id":"step-1","action":"run","summary":"work done"}
{"ts":"2026-07-23T10:20:00Z","step_id":"step-2","action":"run","summary":"all finished"}
`
	if err := os.WriteFile(tracePath, []byte(traceContent), 0o644); err != nil {
		t.Fatalf("write trace: %v", err)
	}
	var out bytes.Buffer
	if err := runDiff(planPath, tracePath, ledgerPath, false, false, true, &out); err != nil {
		t.Errorf("expected nil for all-matched trace with --fail-on-drift, got %v", err)
	}
}

// TestRunDiffFailOnDriftAcceptedDoesNotGate (v0.5.0 feat-diff-exit-code-on-drift):
// a deviation folded by the accepted overlay (an accepted drifting step) does
// NOT trip the gate — only UNACCEPTED drift does. This is the CI complement
// to the accept→patch→accept loop: an accepted drift is acknowledged, not a
// build failure.
func TestRunDiffFailOnDriftAcceptedDoesNotGate(t *testing.T) {
	dir := t.TempDir()
	planPath := filepath.Join(dir, "plan.md")
	tracePath := filepath.Join(dir, "trace.jsonl")
	ledgerPath := filepath.Join(dir, "ledger.jsonl")
	planMD := `# Plan: demo
version: 0.1.0
## step-1
intent: do thing
accept: done
## step-2
intent: do other
accept: finished
`
	if err := os.WriteFile(planPath, []byte(planMD), 0o644); err != nil {
		t.Fatalf("write plan: %v", err)
	}
	// step-2 is drifting, but the user accepted it (accept overlay) → must
	// NOT trip the gate under --fail-on-drift.
	traceContent := `{"ts":"2026-07-23T10:00:00Z","step_id":"step-1","action":"run","summary":"work done"}
{"ts":"2026-07-23T10:20:00Z","step_id":"step-2","action":"run","summary":"punted on the work"}
`
	if err := os.WriteFile(tracePath, []byte(traceContent), 0o644); err != nil {
		t.Fatalf("write trace: %v", err)
	}
	l := ledger.New(ledgerPath)
	if err := l.Accept("0.1.0", diff.Deviation{
		StepID: "step-2",
		Kind:   diff.KindDrifting,
	}); err != nil {
		t.Fatalf("Accept: %v", err)
	}
	var out bytes.Buffer
	if err := runDiff(planPath, tracePath, ledgerPath, false, false, true, &out); err != nil {
		t.Errorf("accepted drifting step should NOT trip --fail-on-drift, got %v", err)
	}
	// And the prose output should still mark step-2 [accepted].
	if !bytes.Contains(out.Bytes(), []byte("[accepted]")) {
		t.Errorf("output should mark the accepted step-2:\n%s", out.String())
	}
}

// TestRunLogPrettyAndJSON (v0.5.0 feat-ledger-log-command): `driftledger log`
// pretty-prints one row per LedgerEntry in append (audit) order, and --json
// round-trips into a slice of ledger.Entry. Fixture: one accept + one patch.
func TestRunLogPrettyAndJSON(t *testing.T) {
	dir := t.TempDir()
	ledgerPath := filepath.Join(dir, "ledger.jsonl")
	l := ledger.New(ledgerPath)
	// one accept + one patch entry, in op order.
	if err := l.Accept("0.1.0", diff.Deviation{
		StepID:  "step-2",
		Kind:    diff.KindDrifting,
		Summary: "punted on statuses",
	}); err != nil {
		t.Fatalf("Accept: %v", err)
	}
	if err := l.Patch("0.1.1", []diff.Deviation{{
		StepID:  "step-2",
		Kind:    diff.KindDrifting,
		Summary: "punted on statuses",
	}}); err != nil {
		t.Fatalf("Patch: %v", err)
	}

	// (a) pretty: two formatted rows in op order (accept before patch).
	var out bytes.Buffer
	if err := runLog(ledgerPath, false, false, &out); err != nil {
		t.Fatalf("runLog: %v", err)
	}
	body := out.Bytes()
	for _, want := range []string{"accept", "patch", "step-2", "0.1.0", "0.1.1", "patched_to:0.1.1"} {
		if !bytes.Contains(body, []byte(want)) {
			t.Errorf("log output missing %q:\n%s", want, out.String())
		}
	}
	// op order: the accept row (plan:0.1.0) precedes the patch row (plan:0.1.1).
	acceptIdx := bytes.Index(body, []byte("plan:0.1.0"))
	patchIdx := bytes.Index(body, []byte("plan:0.1.1"))
	if acceptIdx < 0 || patchIdx < 0 {
		t.Fatalf("expected both accept and patch rows; indices accept=%d patch=%d", acceptIdx, patchIdx)
	}
	if acceptIdx > patchIdx {
		t.Errorf("accept row should precede patch row (audit order): accept@%d patch@%d", acceptIdx, patchIdx)
	}

	// (b) --json: round-trips into []ledger.Entry, op order preserved, patch
	// entry stamped with patched_to_version.
	var jout bytes.Buffer
	if err := runLog(ledgerPath, true, false, &jout); err != nil {
		t.Fatalf("runLog --json: %v", err)
	}
	var got []ledger.Entry
	if err := json.Unmarshal(jout.Bytes(), &got); err != nil {
		t.Fatalf("--json output is not valid JSON / does not round-trip into []ledger.Entry: %v\noutput:\n%s", err, jout.String())
	}
	if len(got) != 2 {
		t.Fatalf("entries = %d, want 2", len(got))
	}
	if got[0].Op != ledger.OpAccept {
		t.Errorf("got[0].op = %q, want accept", got[0].Op)
	}
	if got[1].Op != ledger.OpPatch {
		t.Errorf("got[1].op = %q, want patch", got[1].Op)
	}
	if got[1].Deviation.PatchedToVersion != "0.1.1" {
		t.Errorf("got[1].patched_to_version = %q, want 0.1.1", got[1].Deviation.PatchedToVersion)
	}
	if got[1].Deviation.StepID != "step-2" {
		t.Errorf("got[1].step_id = %q, want step-2", got[1].Deviation.StepID)
	}
}

// TestRunLogEmptyLedgerIsFine (v0.5.0): a fresh repo with no ledger is not an
// error — `driftledger log` prints a zero-entry summary (mirrors
// ledger.Read's nil/nil contract for a missing file).
func TestRunLogEmptyLedgerIsFine(t *testing.T) {
	dir := t.TempDir()
	ledgerPath := filepath.Join(dir, "ledger.jsonl")
	var out bytes.Buffer
	if err := runLog(ledgerPath, false, false, &out); err != nil {
		t.Fatalf("runLog on missing ledger: %v", err)
	}
	if !bytes.Contains(out.Bytes(), []byte("0 entries")) {
		t.Errorf("log of empty ledger should report 0 entries:\n%s", out.String())
	}
}

// TestRunLogJSONEmptyLedgerEmitsArray (v0.6.0 fix-log-json-null-on-empty-ledger):
// `driftledger log --json` on a missing/empty ledger must emit the JSON array
// literal `[]`, not `null`. Before the fix ledger.Read returned a nil []Entry
// for a missing file and json.Marshal rendered nil as `null`, breaking naive
// machine consumers (e.g. Python `for e in json.loads(stdout)` crashes on null
// where it expects a list) and violating the --json flag's "emit the ledger as
// a JSON array" contract.
func TestRunLogJSONEmptyLedgerEmitsArray(t *testing.T) {
	dir := t.TempDir()
	ledgerPath := filepath.Join(dir, "ledger.jsonl") // missing — fresh repo
	var out bytes.Buffer
	if err := runLog(ledgerPath, true, false, &out); err != nil {
		t.Fatalf("runLog --json on missing ledger: %v", err)
	}
	got := bytes.TrimSpace(out.Bytes())
	if !bytes.Equal(got, []byte("[]")) {
		t.Errorf("log --json on empty ledger = %q, want %q (null breaks naive JSON-array consumers)", string(got), "[]")
	}
	// And it must round-trip into an empty (non-nil) slice, not a nil one.
	var entries []ledger.Entry
	if err := json.Unmarshal(got, &entries); err != nil {
		t.Fatalf("output is not valid JSON: %v\noutput: %s", err, got)
	}
	if len(entries) != 0 {
		t.Errorf("entries = %d, want 0", len(entries))
	}
}

// TestRunDiffSurfacesLedgerReadError (v0.6.0 fix-diff-swallows-ledger-read-error):
// a non-NotExist ledger-read error (here: ledger path is a directory, so the
// bufio scan fails with EISDIR) must be surfaced by runDiff, not swallowed.
// Before the fix runDiff set accepted=nil and continued on ANY ledger error,
// silently dropping the accept overlay so previously-accepted drift showed as
// unaccepted — and under --fail-on-drift (the v0.5.0 CI gate) the build failed
// spuriously. A directory is used (rather than chmod 0) so the failure is
// portable and root-bypass-free: os.Open succeeds on a directory, then the
// scan read returns EISDIR, a non-NotExist error that reaches the guard.
func TestRunDiffSurfacesLedgerReadError(t *testing.T) {
	dir := t.TempDir()
	planPath := filepath.Join(dir, "plan.md")
	tracePath := filepath.Join(dir, "trace.jsonl")
	ledgerPath := filepath.Join(dir, "ledgerdir") // a DIRECTORY, not a file
	if err := os.Mkdir(ledgerPath, 0o755); err != nil {
		t.Fatalf("mkdir ledger dir: %v", err)
	}
	planMD := `# Plan: demo
version: 0.1.0
## step-1
intent: do thing
accept: done
## step-2
intent: do other
accept: finished
`
	if err := os.WriteFile(planPath, []byte(planMD), 0o644); err != nil {
		t.Fatalf("write plan: %v", err)
	}
	// step-1 matched; step-2 drifting. If the ledger read were swallowed, the
	// accept overlay would be nil and step-2 would count as unaccepted drift.
	traceContent := `{"ts":"2026-07-23T10:00:00Z","step_id":"step-1","action":"run","summary":"work done"}
{"ts":"2026-07-23T10:20:00Z","step_id":"step-2","action":"run","summary":"punted on the work"}
`
	if err := os.WriteFile(tracePath, []byte(traceContent), 0o644); err != nil {
		t.Fatalf("write trace: %v", err)
	}

	// Without --fail-on-drift: the swallowed path used to return nil; now the
	// ledger-read error surfaces directly.
	var out bytes.Buffer
	err := runDiff(planPath, tracePath, ledgerPath, false, false, false, &out)
	if err == nil {
		t.Fatal("expected runDiff to surface the ledger-read error, got nil (swallowed)")
	}
	if !bytes.Contains([]byte(err.Error()), []byte("ledger")) {
		t.Errorf("error should reference the ledger read failure, got: %v", err)
	}

	// Under --fail-on-drift the OLD code spuriously fired the drift gate
	// (step-2 unaccepted) because the accept overlay was dropped; the fix
	// returns the ledger error instead, so the gate never fires on a read
	// failure.
	out.Reset()
	err = runDiff(planPath, tracePath, ledgerPath, false, false, true, &out)
	if err == nil {
		t.Fatal("expected runDiff to surface the ledger-read error under --fail-on-drift, got nil")
	}
	if bytes.Contains([]byte(err.Error()), []byte("unaccepted deviation")) {
		t.Errorf("--fail-on-drift spuriously fired the drift gate on a ledger-read error (accept overlay dropped): %v", err)
	}
}
