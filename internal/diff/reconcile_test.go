package diff

import (
	"testing"
	"time"

	"github.com/SuperMarioYL/driftledger/internal/plan"
	"github.com/SuperMarioYL/driftledger/internal/trace"
)

func mustPlan(t *testing.T, md string) *plan.PlanContract {
	t.Helper()
	c, err := plan.ParseMarkdown(md)
	if err != nil {
		t.Fatalf("plan parse: %v", err)
	}
	return c
}

func ev(ts, step, action, summary string) trace.Event {
	t, err := time.Parse(time.RFC3339, ts)
	if err != nil {
		panic(err)
	}
	return trace.Event{TS: t, StepID: step, Action: action, Summary: summary}
}

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

func TestReconcileAllMatched(t *testing.T) {
	p := mustPlan(t, planMD)
	events := []trace.Event{
		ev("2026-07-23T10:00:00Z", "step-1", "run", "initialized go module and added cmd package"),
		ev("2026-07-23T10:05:00Z", "step-2", "run", "reconciler now reports matched and drifting status"),
		ev("2026-07-23T10:10:00Z", "step-3", "run", "reconcile_test passes locally"),
	}
	devs := Reconcile(p, events)
	if len(devs) != 3 {
		t.Fatalf("deviations = %d, want 3", len(devs))
	}
	for _, d := range devs {
		if d.Kind != KindMatched {
			t.Errorf("%s kind = %s, want matched", d.StepID, d.Kind)
		}
	}
}

func TestReconcileUnexecutedAndDrifting(t *testing.T) {
	p := mustPlan(t, planMD)
	events := []trace.Event{
		ev("2026-07-23T10:00:00Z", "step-1", "run", "initialized go module and added cmd package"),
		// step-2 ran but its summaries never mention "matched" or "drifting" as
		// whole words ("statuses" is not "status") → drifting.
		ev("2026-07-23T10:05:00Z", "step-2", "run", "started sketching a reconciler, punted on statuses"),
		// step-3 never appears → unexecuted.
	}
	devs := Reconcile(p, events)
	byID := map[string]Deviation{}
	for _, d := range devs {
		byID[d.StepID] = d
	}
	if byID["step-1"].Kind != KindMatched {
		t.Errorf("step-1 kind = %s, want matched", byID["step-1"].Kind)
	}
	if byID["step-2"].Kind != KindDrifting {
		t.Errorf("step-2 kind = %s, want drifting", byID["step-2"].Kind)
	}
	if len(byID["step-2"].UnmetCriteria) == 0 {
		t.Error("step-2 should list unmet criteria")
	}
	if byID["step-3"].Kind != KindUnexecuted {
		t.Errorf("step-3 kind = %s, want unexecuted", byID["step-3"].Kind)
	}
	if !byID["step-2"].FirstSeenTS.Equal(time.Date(2026, 7, 23, 10, 5, 0, 0, time.UTC)) {
		t.Errorf("step-2 first_seen = %v", byID["step-2"].FirstSeenTS)
	}
}

func TestReconcileExtra(t *testing.T) {
	p := mustPlan(t, planMD)
	events := []trace.Event{
		ev("2026-07-23T10:00:00Z", "step-1", "run", "initialized go module and added cmd package"),
		// An event with a step id NOT in the plan.
		ev("2026-07-23T10:20:00Z", "side-quest", "run", "rewrote the README hero instead"),
		// An event with NO step id.
		ev("2026-07-23T10:21:00Z", "", "note", "reading unrelated blog posts"),
	}
	devs := Reconcile(p, events)
	var extras []Deviation
	for _, d := range devs {
		if d.Kind == KindExtra {
			extras = append(extras, d)
		}
	}
	if len(extras) != 2 {
		t.Fatalf("extras = %d, want 2", len(extras))
	}
	if extras[0].FirstSeenTS.After(extras[1].FirstSeenTS) {
		t.Error("extras should be ordered by first-seen ts ascending")
	}
}

func TestReconcileOrderMatchesPlan(t *testing.T) {
	p := mustPlan(t, planMD)
	devs := Reconcile(p, nil)
	if len(devs) != 3 {
		t.Fatalf("deviations = %d, want 3", len(devs))
	}
	want := []string{"step-1", "step-2", "step-3"}
	for i, d := range devs {
		if d.StepID != want[i] {
			t.Errorf("dev[%d].StepID = %s, want %s", i, d.StepID, want[i])
		}
		if d.Kind != KindUnexecuted {
			t.Errorf("dev[%d].Kind = %s, want unexecuted", i, d.Kind)
		}
	}
}

func TestReconcileNilPlan(t *testing.T) {
	if devs := Reconcile(nil, nil); devs != nil {
		t.Errorf("nil plan should yield nil deviations, got %v", devs)
	}
}

func TestCriterionSatisfied(t *testing.T) {
	words := summaryWordSet([]trace.Event{
		{Summary: "initialized go module and added the cmd package"},
	})
	cases := []struct {
		crit string
		want bool
	}{
		{"go module", true},      // "module" present ("go" is len-2, dropped)
		{"cmd package", true},   // both whole words present
		{"cmd present", false},    // "present" is not a whole word in the summary
		{"module", true},        // single token present
		{"nonexistent", false},   // absent
		{"", true},              // no significant tokens → vacuously satisfied
		{"the", true},           // "the" is a stopword → no significant tokens → vacuous
	}
	for _, c := range cases {
		if got := criterionSatisfied(c.crit, words); got != c.want {
			t.Errorf("criterionSatisfied(%q) = %v, want %v", c.crit, got, c.want)
		}
	}
}

// TestWholeWordNotSubstring guards the design choice that "status" must NOT
// match "statuses" — that distinction is exactly what flags drift.
func TestWholeWordNotSubstring(t *testing.T) {
	words := summaryWordSet([]trace.Event{{Summary: "punted on statuses"}})
	if criterionSatisfied("status", words) {
		t.Error(`"status" must not match "statuses" (whole-word, not substring)`)
	}
}

func TestOverlayAccepted(t *testing.T) {
	p := mustPlan(t, planMD)
	events := []trace.Event{
		ev("2026-07-23T10:00:00Z", "step-1", "run", "initialized go module and added cmd package"),
		ev("2026-07-23T10:05:00Z", "step-2", "run", "sketching reconciler, no statuses yet"),
	}
	devs := Reconcile(p, events)
	devs = OverlayAccepted(devs, map[string]bool{"step-2": true})
	byID := map[string]Deviation{}
	for _, d := range devs {
		byID[d.StepID] = d
	}
	if byID["step-2"].Accepted != true {
		t.Error("step-2 should be accepted after overlay")
	}
	if byID["step-1"].Accepted != false {
		t.Error("step-1 should not be accepted")
	}
}

func TestFormatDeviation(t *testing.T) {
	d := Deviation{
		StepID:      "step-2",
		Kind:        KindDrifting,
		FirstSeenTS: time.Date(2026, 7, 23, 10, 5, 0, 0, time.UTC),
		UnmetCriteria: []string{"matched status"},
	}
	s := FormatDeviation(d)
	if s == "" {
		t.Error("FormatDeviation returned empty string")
	}
	if !contains(s, "drifting") {
		t.Errorf("output missing kind: %q", s)
	}
	if !contains(s, "unmet: matched status") {
		t.Errorf("output missing unmet: %q", s)
	}
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
