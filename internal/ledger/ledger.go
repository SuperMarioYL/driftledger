// Package ledger is the append-only JSONL audit trail of the deviation ledger.
//
// The ledger is the binding half of the plan-execution-deviation-ledger primitive:
// every accept / patch / rollback is appended as one JSON line to
// `./driftledger.ledger.jsonl`, so the trail is `jq`-inspectable with zero
// dependencies. m1 only writes `accept` entries; `patch` (m2) and `rollback`
// (m3) share the same schema so the file is forward-compatible.
package ledger

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"sync"
	"time"

	"github.com/SuperMarioYL/driftledger/internal/diff"
)

// Op is the kind of ledger entry. m1 only emits Accept; Patch and Rollback are
// stubbed so the schema is fixed before m2/m3 land.
type Op string

const (
	OpAccept   Op = "accept"
	OpPatch    Op = "patch"
	OpRollback Op = "rollback"
)

// Entry is one append-only ledger line. It mirrors the LedgerEntry pseudo-type
// in mvp_plan §2.
type Entry struct {
	TS          time.Time      `json:"ts"`
	PlanVersion string         `json:"plan_version"`
	Op          Op             `json:"op"`
	Deviation   diff.Deviation `json:"deviation"`
}

// Ledger is a file-backed append-only log. It is safe for concurrent appends
// (the watch TUI accepts from the main goroutine while a tail poller reads).
type Ledger struct {
	path string
	mu   sync.Mutex
}

// New opens (lazily creating) an append-only ledger at path.
func New(path string) *Ledger {
	return &Ledger{path: path}
}

// Path returns the ledger file path so callers can re-read accepted state.
func (l *Ledger) Path() string {
	return l.path
}

// Append writes one entry as a single JSONL line. The file is opened in
// append-only mode so crashes mid-run never corrupt earlier entries.
func (l *Ledger) Append(e Entry) error {
	if e.TS.IsZero() {
		e.TS = time.Now().UTC()
	}
	data, err := json.Marshal(e)
	if err != nil {
		return fmt.Errorf("ledger: marshal: %w", err)
	}
	data = append(data, '\n')

	l.mu.Lock()
	defer l.mu.Unlock()
	f, err := os.OpenFile(l.path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return fmt.Errorf("ledger: open %s: %w", l.path, err)
	}
	defer f.Close()
	if _, err := f.Write(data); err != nil {
		return fmt.Errorf("ledger: write: %w", err)
	}
	return nil
}

// Accept is a convenience that appends an `accept` entry for a deviation.
func (l *Ledger) Accept(planVersion string, d diff.Deviation) error {
	return l.Append(Entry{
		PlanVersion: planVersion,
		Op:          OpAccept,
		Deviation:   d,
	})
}

// Patch appends a `patch` LedgerEntry (op: patch, plan_version: <newVersion>)
// for every accepted deviation folded into the new plan contract, each stamped
// with PatchedToVersion. When there are no accepted deviations it still appends
// a single plan-level marker entry so the ledger records the version bump.
// This turns the append-only ledger from a view into a versioned audit trail:
// `jq '.op=="patch"' ledger.jsonl` lists every contract revision and the
// deviations folded into it. Realizes the base plan's m2_patch_contract
// milestone alongside `driftledger patch`.
func (l *Ledger) Patch(planVersion string, devs []diff.Deviation) error {
	if len(devs) == 0 {
		return l.Append(Entry{
			PlanVersion: planVersion,
			Op:          OpPatch,
			Deviation: diff.Deviation{
				Summary: fmt.Sprintf("patched plan to %s (no accepted deviations)", planVersion),
			},
		})
	}
	for _, d := range devs {
		d.PatchedToVersion = planVersion
		if err := l.Append(Entry{
			PlanVersion: planVersion,
			Op:          OpPatch,
			Deviation:   d,
		}); err != nil {
			return err
		}
	}
	return nil
}

// Rollback appends a `rollback` LedgerEntry (op: rollback) for every accepted
// deviation being reverted, closing the patch/accept/rollback loop. When there
// are no accepted deviations it still appends a single marker entry so the
// ledger records the rollback event. Realizes the base plan's m3_emit_rollback
// milestone: `driftledger rollback` emits directives (never executes them) and
// records the revert in the versioned audit trail. `pendingAccepted` treats an
// OpRollback entry the same as OpPatch (resets the pending set).
func (l *Ledger) Rollback(planVersion string, devs []diff.Deviation) error {
	if len(devs) == 0 {
		return l.Append(Entry{
			PlanVersion: planVersion,
			Op:          OpRollback,
			Deviation: diff.Deviation{
				Summary: fmt.Sprintf("rollback at %s (no accepted deviations)", planVersion),
			},
		})
	}
	for _, d := range devs {
		if err := l.Append(Entry{
			PlanVersion: planVersion,
			Op:          OpRollback,
			Deviation:   d,
		}); err != nil {
			return err
		}
	}
	return nil
}

// Read returns every entry in the ledger file. A missing file returns nil +
// nil — a fresh repo simply has no accepted deviations yet.
func Read(path string) ([]Entry, error) {
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("ledger: open %s: %w", path, err)
	}
	defer f.Close()

	var entries []Entry
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for sc.Scan() {
		line := sc.Text()
		if line == "" {
			continue
		}
		var e Entry
		if err := json.Unmarshal([]byte(line), &e); err != nil {
			// Skip a malformed line rather than aborting the whole trail — the
			// ledger must stay readable even if a writer crashed mid-line.
			continue
		}
		entries = append(entries, e)
	}
	if err := sc.Err(); err != nil {
		return entries, fmt.Errorf("ledger: scan: %w", err)
	}
	return entries, nil
}

// AcceptedStepIDs reads the ledger and returns the set of plan step ids that
// have an `accept` entry recorded SINCE the most recent patch/rollback. The
// watch TUI overlays this onto a fresh reconcile so a previously-accepted
// drift stays accepted as new trace lines arrive.
//
// A patch folds the pending accepts into a new plan version and a rollback
// reverts them; both CONSUME the accepted set, so the overlay is reset on an
// OpPatch/OpRollback entry — mirroring pendingAccepted's reset — so only
// accepts recorded since the last patch/rollback remain. Without this reset a
// step accepted pre-patch would stay marked [accepted] in the diff/watch
// overlay after the patch, masking post-patch drift on the same step as
// already-accepted and blocking re-acceptance — breaking the
// accept→patch→accept loop that is the product's core workflow
// (v0.5.0 fix-accepted-overlay-leaks-past-patch).
func AcceptedStepIDs(path string) (map[string]bool, error) {
	entries, err := Read(path)
	if err != nil {
		return nil, err
	}
	accepted := make(map[string]bool)
	for _, e := range entries {
		switch e.Op {
		case OpPatch, OpRollback:
			// The patch/rollback consumed the pending accepts; only accepts
			// recorded AFTER this entry remain in the overlay set.
			accepted = make(map[string]bool)
		case OpAccept:
			if e.Deviation.StepID != "" {
				accepted[e.Deviation.StepID] = true
			}
		}
	}
	return accepted, nil
}
