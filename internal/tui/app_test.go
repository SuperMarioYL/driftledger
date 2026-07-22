package tui

import (
	"os"
	"path/filepath"
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
