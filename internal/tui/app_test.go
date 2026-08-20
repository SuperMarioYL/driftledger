package tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/SuperMarioYL/driftledger/internal/diff"
	"github.com/SuperMarioYL/driftledger/internal/ledger"
)

const planMD = `# Plan: demo
version: 0.1.0
## step-1
intent: Scaffold the project structure
accept: go module
accept: cmd package
## step-2
intent: Implement the reconciler
accept: matched
accept: drifting
## step-3
intent: Write tests covering the deviation diff
accept: reconcile_test
`

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func traceLine(ts, step, action, summary string) string {
	return `{"ts":"` + ts + `","step_id":"` + step + `","action":"` + action + `","summary":"` + summary + `"}` + "\n"
}

// newModel builds a Model against temp files so tests never touch the real fs.
func newModel(t *testing.T, traceContent string) (Model, string, string, string) {
	t.Helper()
	dir := t.TempDir()
	planPath := filepath.Join(dir, "plan.md")
	tracePath := filepath.Join(dir, "trace.jsonl")
	ledgerPath := filepath.Join(dir, "ledger.jsonl")
	writeFile(t, planPath, planMD)
	writeFile(t, tracePath, traceContent)
	m, err := New(planPath, tracePath, ledgerPath)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return m, planPath, tracePath, ledgerPath
}

func keyMsg(r rune) tea.Msg {
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}}
}

func TestNewPaintsUnexecutedWhenTraceEmpty(t *testing.T) {
	m, _, _, _ := newModel(t, "")
	if len(m.deviations) != 3 {
		t.Fatalf("deviations = %d, want 3", len(m.deviations))
	}
	for _, d := range m.deviations {
		if d.Kind != diff.KindUnexecuted {
			t.Errorf("%s kind = %s, want unexecuted", d.StepID, d.Kind)
		}
	}
}

func TestRefreshReconcilesTrace(t *testing.T) {
	m, _, tracePath, _ := newModel(t, traceLine(
		"2026-07-23T10:00:00Z", "step-1", "run",
		"initialized go module and added cmd package"))

	// Append step-2 drifting + an extra tangent mid-flight.
	content := traceLine("2026-07-23T10:00:00Z", "step-1", "run",
		"initialized go module and added cmd package")
	content += traceLine("2026-07-23T10:20:00Z", "step-2", "run",
		"started a reconciler, punted on statuses")
	content += traceLine("2026-07-23T10:20:30Z", "side-quest", "run",
		"rewrote the README hero instead")
	writeFile(t, tracePath, content)

	// A tick should pick up the growth and re-reconcile.
	mm, _ := m.Update(tickMsg(time.Now()))
	m = mm.(Model)

	byID := map[string]diff.Deviation{}
	var extras int
	for _, d := range m.deviations {
		if d.Kind == diff.KindExtra {
			extras++
		} else {
			byID[d.StepID] = d
		}
	}
	if byID["step-1"].Kind != diff.KindMatched {
		t.Errorf("step-1 = %s, want matched", byID["step-1"].Kind)
	}
	if byID["step-2"].Kind != diff.KindDrifting {
		t.Errorf("step-2 = %s, want drifting", byID["step-2"].Kind)
	}
	if byID["step-3"].Kind != diff.KindUnexecuted {
		t.Errorf("step-3 = %s, want unexecuted", byID["step-3"].Kind)
	}
	if extras != 1 {
		t.Errorf("extras = %d, want 1", extras)
	}
}

func TestAcceptAppendsLedgerEntry(t *testing.T) {
	m, _, tracePath, ledgerPath := newModel(t, traceLine(
		"2026-07-23T10:20:00Z", "step-2", "run",
		"started a reconciler, punted on statuses"))
	// step-1 and step-3 are unexecuted; cursor lands on step-1 (index 0).
	// Move down to step-2 (drifting) at index 1.
	mm, _ := m.Update(keyMsg('j'))
	m = mm.(Model)
	if m.cursor != 1 {
		t.Fatalf("cursor = %d, want 1", m.cursor)
	}

	mm, _ = m.Update(keyMsg('a'))
	m = mm.(Model)
	if m.deviations[1].Accepted != true {
		t.Fatal("step-2 should be marked accepted after `a`")
	}

	// The ledger file should now carry exactly one accept entry for step-2.
	accepted, err := ledger.AcceptedStepIDs(ledgerPath)
	if err != nil {
		t.Fatalf("AcceptedStepIDs: %v", err)
	}
	if !accepted["step-2"] {
		t.Errorf("ledger accepted set = %v, want step-2", accepted)
	}

	// A second `a` on the same (now-accepted) step must NOT append a duplicate.
	mm, _ = m.Update(keyMsg('a'))
	m = mm.(Model)
	entries, _ := ledger.Read(ledgerPath)
	if len(entries) != 1 {
		t.Errorf("ledger entries = %d, want 1 (no duplicate accept)", len(entries))
	}
	_ = tracePath
}

func TestAcceptIsIdempotentAcrossRefresh(t *testing.T) {
	m, _, _, ledgerPath := newModel(t, traceLine(
		"2026-07-23T10:20:00Z", "step-2", "run",
		"started a reconciler, punted on statuses"))
	// move to step-2 (cursor 1) and accept
	mm, _ := m.Update(keyMsg('j'))
	m = mm.(Model)
	mm, _ = m.Update(keyMsg('a'))
	m = mm.(Model)
	if !m.deviations[1].Accepted {
		t.Fatal("expected accepted before refresh")
	}
	// A refresh must preserve the accepted overlay from the ledger.
	m.refresh()
	if !m.deviations[1].Accepted {
		t.Fatal("accepted state lost across refresh")
	}
	_ = ledgerPath
}

func TestQuitOnQ(t *testing.T) {
	m, _, _, _ := newModel(t, "")
	mm, cmd := m.Update(keyMsg('q'))
	m = mm.(Model)
	if !m.quitting {
		t.Error("model should be quitting after q")
	}
	if cmd == nil {
		t.Error("q should return a tea.Quit command")
	}
}

func TestQuitOnCtrlC(t *testing.T) {
	m, _, _, _ := newModel(t, "")
	mm, _ := m.Update(tea.KeyMsg{Type: tea.KeyCtrlC})
	m = mm.(Model)
	if !m.quitting {
		t.Error("model should be quitting after Ctrl+C")
	}
}

func TestViewContainsStatuses(t *testing.T) {
	m, _, _, _ := newModel(t, traceLine(
		"2026-07-23T10:00:00Z", "step-1", "run",
		"initialized go module and added cmd package"))
	out := m.View()
	for _, want := range []string{"DriftLedger", "matched", "unexecuted", "navigate"} {
		if !contains(out, want) {
			t.Errorf("View missing %q\n%s", want, out)
		}
	}
}

func TestNewRejectsBadPlan(t *testing.T) {
	dir := t.TempDir()
	planPath := filepath.Join(dir, "plan.md")
	// A plan with no steps is a parse error → New must surface it.
	writeFile(t, planPath, "# just a title\n")
	if _, err := New(planPath, "", filepath.Join(dir, "l.jsonl")); err == nil {
		t.Fatal("expected error for plan with no steps, got nil")
	}
}

// TestRefreshSurfacesTooLongLineError is the regression for
// fix-tui-trace-read-error-swallow: a single trace line exceeding the 1MB
// scanner buffer must surface the read error in View() (not swallow it) and
// must NOT silently reconcile the partial events parsed before the too-long
// line, which previously painted an incomplete ledger with no error signal.
func TestRefreshSurfacesTooLongLineError(t *testing.T) {
	dir := t.TempDir()
	planPath := filepath.Join(dir, "plan.md")
	tracePath := filepath.Join(dir, "trace.jsonl")
	ledgerPath := filepath.Join(dir, "ledger.jsonl")
	writeFile(t, planPath, planMD)
	// A valid step-1 event, then a single trace line whose payload exceeds the
	// 1MB scanner buffer. ParseFile returns the partial step-1 event plus
	// bufio.ErrTooLong; the bug fed the partial event into diff.Reconcile
	// (step-1 painted matched) and then wiped m.err — a silently partial ledger
	// with nothing wrong shown.
	giant := `{"ts":"2026-07-23T10:05:00Z","step_id":"step-2","action":"run","summary":"` +
		strings.Repeat("x", 2*1024*1024) + `"}`
	content := traceLine("2026-07-23T10:00:00Z", "step-1", "run",
		"initialized go module and added cmd package") + giant + "\n"
	writeFile(t, tracePath, content)

	m, err := New(planPath, tracePath, ledgerPath)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	// (a) the trace-read error surfaces in View() rather than being swallowed.
	if m.err == "" {
		t.Fatal("expected trace-read error to surface in m.err, got empty")
	}
	out := m.View()
	if !contains(out, "trace read") {
		t.Errorf("View missing trace-read error:\n%s", out)
	}
	// (b) partial events are not silently reconciled on the too-long path:
	// step-1's valid event must not paint as matched (reconcile was halted).
	for _, d := range m.deviations {
		if d.StepID == "step-1" && d.Kind == diff.KindMatched {
			t.Fatalf("partial step-1 event reconciled as matched on the too-long path: %v", m.deviations)
		}
	}
}

func TestNewAcceptsMissingTrace(t *testing.T) {
	dir := t.TempDir()
	planPath := filepath.Join(dir, "plan.md")
	writeFile(t, planPath, planMD)
	// A nonexistent trace file is fine — every step paints as unexecuted.
	m, err := New(planPath, filepath.Join(dir, "absent.jsonl"), filepath.Join(dir, "l.jsonl"))
	if err != nil {
		t.Fatalf("New with missing trace: %v", err)
	}
	if len(m.deviations) != 3 {
		t.Fatalf("deviations = %d, want 3", len(m.deviations))
	}
}

// TestOverlayAcceptedSurfacesLedgerReadError (v0.6.0
// fix-diff-swallows-ledger-read-error): a non-NotExist ledger-read error (here:
// the ledger path is a directory, so the bufio scan fails with EISDIR) must
// surface in m.err, mirroring the trace-read guard — not be swallowed. Before
// the fix overlayAccepted returned early on ANY ledger error WITHOUT setting
// m.err, so the live ledger silently lost accepted state with no error band.
// A directory is used (rather than chmod 0) so the failure is portable and
// root-bypass-free: os.Open succeeds on a directory, then the scan read returns
// EISDIR, a non-NotExist error that reaches the guard.
func TestOverlayAcceptedSurfacesLedgerReadError(t *testing.T) {
	dir := t.TempDir()
	planPath := filepath.Join(dir, "plan.md")
	tracePath := filepath.Join(dir, "trace.jsonl")
	ledgerPath := filepath.Join(dir, "ledgerdir") // a DIRECTORY, not a file
	if err := os.Mkdir(ledgerPath, 0o755); err != nil {
		t.Fatalf("mkdir ledger dir: %v", err)
	}
	writeFile(t, planPath, planMD)
	// A clean first trace: step-1 matched, step-2/3 unexecuted — no trace error,
	// so any m.err set after refresh must come from the ledger-read path.
	writeFile(t, tracePath, traceLine("2026-07-23T10:00:00Z", "step-1", "run",
		"initialized go module and added cmd package"))
	m, err := New(planPath, tracePath, ledgerPath)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if m.err == "" {
		t.Fatal("expected a ledger-read error to surface in m.err, got empty (swallowed)")
	}
	if !contains(m.err, "ledger read") {
		t.Errorf("m.err should mention the ledger read failure: %q", m.err)
	}
	// View must render the error band so the failure is visible, not silent.
	out := m.View()
	if !contains(out, "ledger read") {
		t.Errorf("View missing the ledger-read error band:\n%s", out)
	}
}

func contains(s, sub string) bool {
	return len(s) >= len(sub) && (func() bool {
		for i := 0; i+len(sub) <= len(s); i++ {
			if s[i:i+len(sub)] == sub {
				return true
			}
		}
		return false
	}())
}

// TestRefreshPreservesDeviationsOnTraceReadError (v0.5.0
// fix-tui-refresh-wipes-on-trace-error): a transient non-NotExist trace-read
// error (here a permission error from os.Open after chmod 0) must preserve
// the last-known-good deviation set and surface the error — NOT fall through
// to diff.Reconcile with nil events, which would silently wipe the live
// ledger to all-unexecuted. The guard previously early-returned only for
// bufio.ErrTooLong, so a permission/IO error fell through and wiped.
func TestRefreshPreservesDeviationsOnTraceReadError(t *testing.T) {
	dir := t.TempDir()
	planPath := filepath.Join(dir, "plan.md")
	tracePath := filepath.Join(dir, "trace.jsonl")
	ledgerPath := filepath.Join(dir, "ledger.jsonl")
	writeFile(t, planPath, planMD)
	// A good first reconcile: step-1 matched, step-2/3 unexecuted.
	goodTrace := traceLine("2026-07-23T10:00:00Z", "step-1", "run",
		"initialized go module and added cmd package")
	writeFile(t, tracePath, goodTrace)
	m, err := New(planPath, tracePath, ledgerPath)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	// Precondition: step-1 painted matched on the good first reconcile.
	if step1 := devFor(m.deviations, "step-1"); step1 == nil || step1.Kind != diff.KindMatched {
		t.Fatalf("precondition: step-1 should be matched after good reconcile, got %v", m.deviations)
	}

	// Grow the trace file so the size short-circuit does not skip the read,
	// then make it unreadable (chmod 0) — a transient permission/IO error
	// from os.Open, the exact gap the fix targets.
	writeFile(t, tracePath, goodTrace+"\n")
	if err := os.Chmod(tracePath, 0o000); err != nil {
		t.Fatalf("chmod trace: %v", err)
	}
	defer os.Chmod(tracePath, 0o644) // restore so TempDir cleanup works

	m.refresh()

	// (a) the read failure must surface in m.err — this also guards against a
	// root-bypass silently making the file readable (the test would fail
	// loudly instead of falsely passing).
	if m.err == "" {
		t.Fatal("expected a trace-read error to surface in m.err, got empty (chmod 0 did not block the read — running as root?)")
	}
	if !contains(m.err, "trace read") {
		t.Errorf("m.err should mention the trace read failure: %q", m.err)
	}
	// (b) the last-known-good deviation set is PRESERVED — step-1 stays
	// matched instead of being wiped to unexecuted by a Reconcile(plan, nil).
	if step1 := devFor(m.deviations, "step-1"); step1 == nil || step1.Kind != diff.KindMatched {
		t.Fatalf("step-1 should stay matched (last-known-good preserved), got %v", m.deviations)
	}
}

// devFor returns the deviation for id (or nil if absent) so a test can assert
// on a single step without re-walking the slice.
func devFor(devs []diff.Deviation, id string) *diff.Deviation {
	for i := range devs {
		if devs[i].StepID == id {
			return &devs[i]
		}
	}
	return nil
}
