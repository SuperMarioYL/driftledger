// Package diff reconciles a versioned plan contract against a live trace.
//
// Reconciliation is STRUCTURAL, not LLM-graded (see mvp_plan §2): step-presence
// plus accept-criteria keyword match. It is deterministic and cheap enough to
// re-run on every new trace line, which is what the live TUI does. Each plan
// step is classified as one of:
//
//   - matched:    the step ran AND every accept criterion's keywords appear in
//     its trace summaries.
//   - drifting:   the step ran but at least one criterion is unsatisfied — the
//     minute-20 catch.
//   - unexecuted: no trace event references the step id.
//   - extra:      a trace event references a step id that is not in the plan
//     (or carries no step id) — work outside the contract.
package diff

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/SuperMarioYL/driftledger/internal/plan"
	"github.com/SuperMarioYL/driftledger/internal/trace"
)

// Kind classifies how a step's execution deviates from the plan contract.
type Kind string

const (
	KindMatched    Kind = "matched"
	KindDrifting   Kind = "drifting"
	KindUnexecuted Kind = "unexecuted"
	KindExtra      Kind = "extra"
)

// Deviation is one row of the deviation ledger. It mirrors the primitive's
// pseudo-type in mvp_plan §2.
type Deviation struct {
	StepID           string    `json:"step_id"`
	Kind             Kind      `json:"kind"`
	FirstSeenTS      time.Time `json:"first_seen_ts"`
	Summary          string    `json:"summary,omitempty"`
	UnmetCriteria    []string  `json:"unmet_criteria,omitempty"`
	Accepted         bool      `json:"accepted"`
	PatchedToVersion string    `json:"patched_to_version,omitempty"`
}

// Reconcile produces the current deviation set for a plan + trace. It is a pure
// function of its inputs — accepted state is overlaid separately from the
// append-only ledger so re-reconciliation on each new trace line is cheap and
// side-effect free.
func Reconcile(p *plan.PlanContract, events []trace.Event) []Deviation {
	if p == nil {
		return nil
	}

	// Bucket events by step id. Events whose step id is absent from the plan
	// (including events with no step id at all) become `extra` deviations.
	byStep := make(map[string][]trace.Event)
	var extra []trace.Event
	for _, ev := range events {
		if ev.StepID != "" {
			if _, inPlan := p.StepByID(ev.StepID); inPlan {
				byStep[ev.StepID] = append(byStep[ev.StepID], ev)
				continue
			}
		}
		extra = append(extra, ev)
	}

	var out []Deviation

	// Plan steps, in declared order, so the ledger reads top-to-bottom like the
	// contract the user wrote.
	for _, step := range p.Steps {
		evs := byStep[step.ID]
		if len(evs) == 0 {
			out = append(out, Deviation{
				StepID:      step.ID,
				Kind:        KindUnexecuted,
				FirstSeenTS: firstTS(evs),
				Summary:     step.Intent,
			})
			continue
		}
		unmet := unmetCriteria(step.AcceptCriteria, evs)
		kind := KindMatched
		if len(unmet) > 0 {
			kind = KindDrifting
		}
		out = append(out, Deviation{
			StepID:        step.ID,
			Kind:          kind,
			FirstSeenTS:   firstTS(evs),
			Summary:       step.Intent,
			UnmetCriteria: unmet,
		})
	}

	// Extra work, ordered by first-seen ts so the tangent reads chronologically.
	sort.SliceStable(extra, func(i, j int) bool {
		return extra[i].TS.Before(extra[j].TS)
	})
	for _, ev := range extra {
		out = append(out, Deviation{
			StepID:      ev.StepID,
			Kind:        KindExtra,
			FirstSeenTS: ev.TS,
			Summary:     ev.Summary,
		})
	}

	return out
}

// OverlayAccepted marks deviations whose step id appears in the accepted set.
// The accepted set is loaded from the append-only ledger; reconciliation itself
// never mutates accepted state.
func OverlayAccepted(devs []Deviation, accepted map[string]bool) []Deviation {
	if len(accepted) == 0 {
		return devs
	}
	for i := range devs {
		if devs[i].Kind == KindExtra {
			continue // acceptance keys off plan step id; extras are transient.
		}
		if accepted[devs[i].StepID] {
			devs[i].Accepted = true
		}
	}
	return devs
}

// unmetCriteria returns the accept criteria that no trace summary satisfies.
func unmetCriteria(criteria []string, evs []trace.Event) []string {
	if len(criteria) == 0 {
		return nil
	}
	words := summaryWordSet(evs)
	var unmet []string
	for _, c := range criteria {
		if !criterionSatisfied(c, words) {
			unmet = append(unmet, c)
		}
	}
	return unmet
}

// criterionSatisfied implements the structural accept-criteria keyword match.
// A criterion is satisfied when every significant token in it (lowercased,
// stopwords dropped, length >= 3) is present as a WHOLE WORD in the step's trace
// summaries. Whole-word (not substring) matching is what keeps "status" from
// matching "statuses" — a structural signal that the agent did not actually
// report the criterion's noun. A criterion with no significant tokens is
// satisfied whenever the step has events (vacuous accept).
//
// This is deliberately structural and deterministic (mvp_plan §2): no LLM
// judgment, runnable on every new trace line. Write each accept criterion as
// the keywords that must appear in the trace when the step is done.
func criterionSatisfied(criterion string, words map[string]bool) bool {
	tokens := tokenize(criterion)
	if len(tokens) == 0 {
		return true
	}
	for _, tok := range tokens {
		if !words[tok] {
			return false
		}
	}
	return true
}

// summaryWordSet lowercases every trace summary and splits it into whole words
// on non-alphanumeric boundaries, keeping `-` and `_` inside a token so names
// like `reconcile_test` survive as one word.
func summaryWordSet(evs []trace.Event) map[string]bool {
	set := make(map[string]bool)
	for _, ev := range evs {
		for _, w := range splitWords(strings.ToLower(ev.Summary)) {
			set[w] = true
		}
	}
	return set
}

func splitWords(s string) []string {
	var out []string
	cur := strings.Builder{}
	flush := func() {
		if cur.Len() > 0 {
			out = append(out, cur.String())
			cur.Reset()
		}
	}
	for _, r := range s {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-' || r == '_' {
			cur.WriteRune(r)
		} else {
			flush()
		}
	}
	flush()
	return out
}

func firstTS(evs []trace.Event) time.Time {
	if len(evs) == 0 {
		return time.Time{}
	}
	first := evs[0].TS
	for _, ev := range evs[1:] {
		if ev.TS.Before(first) {
			first = ev.TS
		}
	}
	return first
}

var stopwords = map[string]struct{}{
	"the": {}, "a": {}, "an": {}, "is": {}, "are": {}, "was": {}, "were": {},
	"be": {}, "been": {}, "to": {}, "of": {}, "in": {}, "on": {}, "for": {},
	"and": {}, "or": {}, "with": {}, "that": {}, "this": {}, "it": {}, "as": {},
	"by": {}, "at": {}, "from": {}, "has": {}, "have": {}, "had": {}, "not": {},
}

// tokenize splits a criterion into lowercase significant tokens (>=3 chars,
// stopwords dropped). Matching is structural, so common words would just add
// noise — the meaningful nouns/verbs are what carry the acceptance signal.
func tokenize(s string) []string {
	s = strings.ToLower(s)
	var out []string
	cur := strings.Builder{}
	flush := func() {
		if cur.Len() >= 3 {
			t := cur.String()
			if _, stop := stopwords[t]; !stop {
				out = append(out, t)
			}
		}
		cur.Reset()
	}
	for _, r := range s {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-' || r == '_' {
			cur.WriteRune(r)
		} else {
			flush()
		}
	}
	flush()
	return out
}

// EmitGitRevertDirective renders a deviation as a copy-pasteable git-revert +
// checkpoint-tag DIRECTIVE. It EMITS the directive as a shell-comment block; it
// NEVER executes it (the base plan keeps auto-execution of rollbacks out of
// scope, §6 — the user reviews and runs the directive manually). Realizes the
// base plan's m3_emit_rollback milestone alongside `driftledger rollback`.
func EmitGitRevertDirective(d Deviation) string {
	id := d.StepID
	if id == "" {
		id = "unplanned"
	}
	ts := "unknown"
	if !d.FirstSeenTS.IsZero() {
		ts = d.FirstSeenTS.Format("20060102-150405")
	}
	summary := d.Summary
	if summary == "" {
		summary = "(no summary)"
	}
	return fmt.Sprintf(`# DriftLedger rollback directive — review then run manually (auto-execution is out of scope, §6).
#   step:      %s (kind: %s, first-seen: %s)
#   summary:   %s
#
#   # 1. revert the commit that introduced the accepted drift:
#   git revert --no-edit <commit-sha>
#
#   # 2. tag the checkpoint so the revert is auditable in the ledger:
#   git tag driftledger-rollback-%s-%s
`, id, d.Kind, ts, summary, id, ts)
}

// FormatDeviation renders one deviation as a single stdout line for the
// `driftledger diff` command.
func FormatDeviation(d Deviation) string {
	ts := "—"
	if !d.FirstSeenTS.IsZero() {
		ts = d.FirstSeenTS.Format(time.RFC3339)
	}
	extra := ""
	if d.Kind == KindDrifting && len(d.UnmetCriteria) > 0 {
		extra = fmt.Sprintf("  unmet: %s", strings.Join(d.UnmetCriteria, "; "))
	}
	if d.Kind == KindExtra {
		extra = fmt.Sprintf("  summary: %s", d.Summary)
	}
	acc := ""
	if d.Accepted {
		acc = "  [accepted]"
	}
	id := d.StepID
	if id == "" {
		id = "(unplanned)"
	}
	return fmt.Sprintf("%-12s %-11s first-seen:%s%s%s", id, d.Kind, ts, extra, acc)
}
