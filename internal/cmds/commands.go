// Package cmds wires the cobra CLI surface: init / diff / watch (m1) plus
// patch / rollback stubs that land in m2 / m3.
package cmds

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/SuperMarioYL/driftledger/internal/diff"
	"github.com/SuperMarioYL/driftledger/internal/ledger"
	"github.com/SuperMarioYL/driftledger/internal/plan"
	"github.com/SuperMarioYL/driftledger/internal/tui"
	"github.com/SuperMarioYL/driftledger/internal/trace"
)

// DefaultLedgerPath is the append-only ledger a `jq` can inspect after a run.
const DefaultLedgerPath = "driftledger.ledger.jsonl"

// renameFn is os.Rename in production; tests may swap it to force a rename
// failure so the rename-failure ledger rollback (v0.5.0
// fix-patch-rename-failure-desync) is exercised without a platform-specific
// filesystem trick (CreateTemp and Rename share the same directory write
// permission, so a portable same-directory rename failure is otherwise
// unforceable from outside the function).
var renameFn = os.Rename

// NewRootCmd builds the top-level `driftledger` command. It is constructed in a
// function so tests and main.go share one wiring point.
func NewRootCmd() *cobra.Command {
	root := &cobra.Command{
		Use:   "driftledger",
		Short: "Plan-execution-deviation-ledger for long-horizon coding/research agents.",
		Long: `DriftLedger turns your agent's stated plan into a versioned contract and
reconciles it against live execution, so you catch and patch drift mid-flight —
not in the post-mortem.

Reconciliation is structural (step-presence plus accept-criteria keyword match),
deterministic, and re-run on every new trace line. The ledger is append-only
JSONL you can inspect with jq. diff + watch ship m1; patch ships m2 (rewrite the
contract from accepted deviations); rollback (m3) is stubbed on the roadmap.`,
		Version: "0.5.0",
	}

	root.AddCommand(newInitCmd())
	root.AddCommand(newDiffCmd())
	root.AddCommand(newWatchCmd())
	root.AddCommand(newPatchCmd())
	root.AddCommand(newRollbackCmd())
	root.AddCommand(newLogCmd())
	return root
}

// Execute runs the root command on os.Args; main.go is a one-liner over this.
func Execute() {
	if err := NewRootCmd().Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

// --- init ----------------------------------------------------------------

func newInitCmd() *cobra.Command {
	var (
		planPath string
		force    bool
	)
	c := &cobra.Command{
		Use:   "init",
		Short: "Scaffold an example plan contract (plan.md) with three steps.",
		RunE: func(cmd *cobra.Command, args []string) error {
			if planPath == "" {
				planPath = "plan.md"
			}
			return writePlan(planPath, force, cmd.OutOrStdout())
		},
	}
	c.Flags().StringVarP(&planPath, "plan", "p", "", "path to write the plan contract (default plan.md)")
	c.Flags().BoolVar(&force, "force", false, "overwrite an existing plan.md")
	return c
}

func writePlan(path string, force bool, out io.Writer) error {
	if !force {
		if _, err := os.Stat(path); err == nil {
			return fmt.Errorf("%s already exists (pass --force to overwrite)", path)
		}
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil && path != filepath.Base(path) {
		// non-fatal: writing plan.md in cwd has no dir to make
	}
	if err := os.WriteFile(path, []byte(plan.DefaultPlanMarkdown), 0o644); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	fmt.Fprintf(out, "wrote %s — edit the steps to match your task, then `driftledger watch %s trace.jsonl`\n", path, path)
	return nil
}

// --- diff ----------------------------------------------------------------

func newDiffCmd() *cobra.Command {
	var (
		ledgerPath  string
		jsonOut     bool
		jsonPretty  bool
		failOnDrift bool
	)
	c := &cobra.Command{
		Use:   "diff <plan.md> <trace.jsonl>",
		Short: "Print plan-vs-trace deviations to stdout (non-interactive).",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runDiff(args[0], args[1], ledgerPath, jsonOut, jsonPretty, failOnDrift, cmd.OutOrStdout())
		},
		// SilenceUsage so a --fail-on-drift exit (or a read error) does not
		// dump the help text alongside the deviation output.
		SilenceUsage: true,
	}
	c.Flags().StringVarP(&ledgerPath, "ledger", "l", DefaultLedgerPath, "deviation ledger path (accept overlay)")
	c.Flags().BoolVar(&jsonOut, "json", false, "emit deviations as a JSON array on stdout (machine-readable)")
	c.Flags().BoolVar(&jsonPretty, "json-pretty", false, "pretty-print the --json output (indent 2 spaces)")
	c.Flags().BoolVar(&failOnDrift, "fail-on-drift", false, "exit non-zero when any unaccepted deviation (drifting/unexecuted/extra) is present (CI gate)")
	return c
}

func runDiff(planPath, tracePath, ledgerPath string, jsonOut, jsonPretty, failOnDrift bool, out io.Writer) error {
	p, err := plan.ParseFile(planPath)
	if err != nil {
		return err
	}
	events, skipped, outOfOrder, err := trace.ParseFile(tracePath)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	// v0.3.0 fix-trace-parsefile-silent-skip: surface malformed-line count so
	// a partial trace is never silently reconciled.
	if skipped > 0 {
		fmt.Fprintf(os.Stderr, "warning: %d unparseable trace line(s) skipped — deviation set may be partial\n", skipped)
	}
	// v0.4.0 feat-trace-out-of-order-ts: surface out-of-order events so a
	// clock-skewed/reordered trace cannot silently mis-rank first-seen.
	if outOfOrder > 0 {
		fmt.Fprintf(os.Stderr, "warning: %d out-of-order trace event(s) (ts precedes the prior event); first-seen ranking still uses the earliest ts\n", outOfOrder)
	}
	devs := diff.Reconcile(p, events)

	accepted, err := ledger.AcceptedStepIDs(ledgerPath)
	if err != nil {
		// A missing ledger is normal for a fresh run — surface nothing, just
		// reconcile without the overlay.
		accepted = nil
	}
	devs = diff.OverlayAccepted(devs, accepted)

	// v0.5.0 feat-diff-exit-code-on-drift: the CI complement to the v0.4.0
	// --json flag — a pipeline consumes the exit code, not the JSON, to fail
	// the build on plan drift. Matched steps, and steps already folded by a
	// patch/accepted overlay (d.Accepted), do not trip the gate; unaccepted
	// drifting/unexecuted/extra do.
	driftCount := 0
	for _, d := range devs {
		if d.Accepted {
			continue
		}
		switch d.Kind {
		case diff.KindDrifting, diff.KindUnexecuted, diff.KindExtra:
			driftCount++
		}
	}

	// v0.4.0 feat-diff-json-output: machine-readable deviation set for CI /
	// agent consumers.
	if jsonOut {
		enc := json.NewEncoder(out)
		if jsonPretty {
			enc.SetIndent("", "  ")
		}
		if err := enc.Encode(devs); err != nil {
			return fmt.Errorf("diff: encode json: %w", err)
		}
		if failOnDrift && driftCount > 0 {
			return fmt.Errorf("diff: %d unaccepted deviation(s) (drifting/unexecuted/extra)", driftCount)
		}
		return nil
	}

	fmt.Fprintf(out, "plan %s  %d steps  %d trace events\n", p.Version, len(p.Steps), len(events))
	fmt.Fprintln(out, "------")
	var counts [4]int
	for _, d := range devs {
		fmt.Fprintln(out, diff.FormatDeviation(d))
		switch d.Kind {
		case diff.KindMatched:
			counts[0]++
		case diff.KindDrifting:
			counts[1]++
		case diff.KindUnexecuted:
			counts[2]++
		case diff.KindExtra:
			counts[3]++
		}
	}
	fmt.Fprintln(out, "------")
	fmt.Fprintf(out, "matched:%d  drifting:%d  unexecuted:%d  extra:%d\n",
		counts[0], counts[1], counts[2], counts[3])
	if failOnDrift && driftCount > 0 {
		return fmt.Errorf("diff: %d unaccepted deviation(s) (drifting/unexecuted/extra)", driftCount)
	}
	return nil
}

// --- watch ---------------------------------------------------------------

func newWatchCmd() *cobra.Command {
	var ledgerPath string
	c := &cobra.Command{
		Use:   "watch <plan.md> <trace.jsonl>",
		Short: "Render the live deviation ledger in a TUI with a-to-accept.",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			return tui.Run(args[0], args[1], ledgerPath)
		},
	}
	c.Flags().StringVarP(&ledgerPath, "ledger", "l", DefaultLedgerPath, "deviation ledger path (accept entries)")
	return c
}

// --- patch (m2) / rollback (m3 stub) -------------------------------------

func newPatchCmd() *cobra.Command {
	var ledgerPath string
	c := &cobra.Command{
		Use:   "patch <plan.md>",
		Short: "Rewrite the plan contract to a new version folding accepted deviations.",
		Long: `Rewrite the plan contract to a new semantic version that captures accepted
deviations as the new contract, and append a ` + "`patch`" + ` LedgerEntry to the
deviation ledger. The ledger becomes a versioned audit trail (jq-inspectable).

Accept a deviation first with ` + "`driftledger watch`" + ` (press ` + "`a`" + `), then run
` + "`driftledger patch plan.md`" + ` to fold the accepted drift into the contract.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runPatch(args[0], ledgerPath, cmd.OutOrStdout())
		},
	}
	c.Flags().StringVarP(&ledgerPath, "ledger", "l", DefaultLedgerPath, "deviation ledger path (patch entry)")
	return c
}

// runPatch realizes the base plan's m2_patch_contract milestone: it bumps the
// plan contract to a new semantic version, folds the accepted deviations that
// landed since the last patch into the contract as `accepted:` annotations,
// and appends a `patch` LedgerEntry per folded deviation (or a single marker
// entry when there are none) so the ledger becomes a versioned audit trail.
// It reads pending accepts straight from the append-only ledger — no trace
// file needed, since each accept entry already carries the deviation snapshot.
func runPatch(planPath, ledgerPath string, out io.Writer) error {
	p, err := plan.ParseFile(planPath)
	if err != nil {
		return err
	}
	entries, err := ledger.Read(ledgerPath)
	if err != nil {
		return err
	}
	pending := pendingAccepted(entries)

	newVersion := plan.BumpVersion(p.Version)
	folds := make([]plan.AcceptedFold, 0, len(pending))
	for _, d := range pending {
		folds = append(folds, plan.AcceptedFold{
			StepID:  d.StepID,
			Kind:    string(d.Kind),
			Summary: d.Summary,
		})
	}

	origMD, err := os.ReadFile(planPath)
	if err != nil {
		return fmt.Errorf("patch: read %s: %w", planPath, err)
	}
	rewritten, err := plan.ApplyPatchMarkdown(string(origMD), newVersion, folds)
	if err != nil {
		return err
	}
	// v0.3.0 fix-patch-plan-ledger-desync: write to a temp file first, append the
	// ledger patch entry, THEN atomically rename — so a ledger-append failure
	// discards the temp file and leaves the plan + pending set unchanged (no
	// double-fold / second version bump on the next patch call).
	tmp, err := os.CreateTemp(filepath.Dir(planPath), ".driftledger-patch-*")
	if err != nil {
		return fmt.Errorf("patch: temp: %w", err)
	}
	tmpPath := tmp.Name()
	cleanup := func() { _ = os.Remove(tmpPath) }
	if _, err := tmp.Write([]byte(rewritten)); err != nil {
		tmp.Close()
		cleanup()
		return fmt.Errorf("patch: write temp: %w", err)
	}
	if err := tmp.Close(); err != nil {
		cleanup()
		return fmt.Errorf("patch: close temp: %w", err)
	}
	l := ledger.New(ledgerPath)
	// v0.5.0 fix-patch-rename-failure-desync: capture the ledger file size
	// before appending the patch entries so a rename failure (plan directory
	// non-writable between CreateTemp and Rename, or a filesystem/SIGINT edge)
	// can roll back the just-appended patch entries. Without this rollback the
	// ledger records patch entries stamping a version the plan never reached,
	// desyncing plan+ledger — the exact failure mode fix-patch-plan-ledger-desync
	// closed for the l.Patch failure path.
	prePatchSize := int64(-1)
	if fi, statErr := os.Stat(ledgerPath); statErr == nil {
		prePatchSize = fi.Size()
	}
	if err := l.Patch(newVersion, pending); err != nil {
		cleanup()
		return fmt.Errorf("patch: ledger: %w", err)
	}
	if err := renameFn(tmpPath, planPath); err != nil {
		// Roll back the just-appended patch entries so the ledger does not
		// record a version the plan never reached (plan+ledger stay in sync).
		if prePatchSize >= 0 {
			_ = os.Truncate(ledgerPath, prePatchSize)
		} else {
			// Ledger did not exist before the patch; remove the freshly-created
			// file so the filesystem state matches the pre-patch state.
			_ = os.Remove(ledgerPath)
		}
		cleanup()
		return fmt.Errorf("patch: rename %s: %w", planPath, err)
	}

	fmt.Fprintf(out, "patched %s → v%s", planPath, newVersion)
	if len(pending) > 0 {
		fmt.Fprintf(out, " (%d accepted deviation(s) folded)", len(pending))
	} else {
		fmt.Fprint(out, " (no pending accepted deviations; version bumped)")
	}
	fmt.Fprintln(out)
	return nil
}

// pendingAccepted returns the accept-entry deviations that have landed since
// the most recent patch entry in the append-only ledger (latest per step id
// wins). A patch entry folds and resets the pending set, so only accepts
// recorded AFTER the last patch remain pending for the next `patch` call.
func pendingAccepted(entries []ledger.Entry) []diff.Deviation {
	var pending []diff.Deviation
	for _, e := range entries {
		switch e.Op {
		case ledger.OpPatch, ledger.OpRollback:
			// A patch folds the pending accepts; a rollback reverts them. Both
			// consume the pending set, so reset so only accepts recorded AFTER
			// the most recent patch/rollback remain pending for the next call.
			// (v0.3.0 impl-rollback-directive: OpRollback must reset exactly
			// like OpPatch, or a post-rollback patch would re-fold reverted
			// deviations.)
			pending = nil
		case ledger.OpAccept:
			pending = replaceDeviation(pending, e.Deviation)
		}
	}
	return pending
}

// replaceDeviation appends d, replacing any existing entry for the same step
// id so the latest accept snapshot wins (the watch TUI is idempotent, but this
// is defensive against duplicate accept entries between patches).
func replaceDeviation(ds []diff.Deviation, d diff.Deviation) []diff.Deviation {
	if d.StepID == "" {
		return append(ds, d)
	}
	for i := range ds {
		if ds[i].StepID == d.StepID {
			ds[i] = d
			return ds
		}
	}
	return append(ds, d)
}

func newRollbackCmd() *cobra.Command {
	var ledgerPath string
	c := &cobra.Command{
		Use:   "rollback <plan.md>",
		Short: "Emit git-revert + checkpoint-tag directives for accepted deviations (never executes them).",
		Long: `Emit a git-revert plus checkpoint-tag DIRECTIVE for each accepted
deviation that landed since the last patch/rollback, and append a ` + "`rollback`" + `
LedgerEntry to the deviation ledger. The directives are printed to stdout for
you to review and run — DriftLedger NEVER executes them (auto-execution of
rollbacks is out of scope). Closes the patch / accept / rollback loop.

Accept a deviation first with ` + "`driftledger watch`" + ` (press ` + "`a`" + `), then run
` + "`driftledger rollback plan.md`" + ` to emit the revert directive.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runRollback(args[0], ledgerPath, cmd.OutOrStdout())
		},
	}
	c.Flags().StringVarP(&ledgerPath, "ledger", "l", DefaultLedgerPath, "deviation ledger path (rollback entry)")
	return c
}

// runRollback realizes the base plan's m3_emit_rollback milestone: it reads the
// pending accepted deviations from the append-only ledger, emits a git-revert +
// checkpoint-tag DIRECTIVE per deviation to stdout (never executes them), and
// appends a `rollback` LedgerEntry per deviation so the ledger records the loop
// close. pendingAccepted treats an OpRollback entry the same as OpPatch (resets
// the pending set), so a rollback consumes reverted accepts.
func runRollback(planPath, ledgerPath string, out io.Writer) error {
	p, err := plan.ParseFile(planPath)
	if err != nil {
		return err
	}
	entries, err := ledger.Read(ledgerPath)
	if err != nil {
		return err
	}
	pending := pendingAccepted(entries)

	if len(pending) == 0 {
		l := ledger.New(ledgerPath)
		if err := l.Rollback(p.Version, nil); err != nil {
			return fmt.Errorf("rollback: ledger: %w", err)
		}
		fmt.Fprintf(out, "no pending accepted deviations; rollback marker recorded at plan %s\n", p.Version)
		return nil
	}

	fmt.Fprintf(out, "# DriftLedger rollback — plan %s, %d accepted deviation(s)\n", p.Version, len(pending))
	fmt.Fprintln(out, "# Review each directive below, then run the git commands manually.")
	fmt.Fprintln(out, "# (Auto-execution of rollbacks is out of scope — §6.)")
	fmt.Fprintln(out, "------")
	for _, d := range pending {
		fmt.Fprint(out, diff.EmitGitRevertDirective(d))
		fmt.Fprintln(out, "------")
	}

	l := ledger.New(ledgerPath)
	if err := l.Rollback(p.Version, pending); err != nil {
		return fmt.Errorf("rollback: ledger: %w", err)
	}

	word := "entries"
	if len(pending) == 1 {
		word = "entry"
	}
	fmt.Fprintf(out, "recorded %d rollback %s in %s\n", len(pending), word, ledgerPath)
	return nil
}

// --- log (v0.5.0 feat-ledger-log-command) -------------------------------

// newLogCmd wires `driftledger log`, a native viewer for the append-only
// ledger. The ledger (./driftledger.ledger.jsonl) is the product's
// versioned-audit-trail primitive, but before v0.5.0 the CLI exposed no
// human-readable viewer — only "any jq can inspect" (base plan §4). A native
// `log` subcommand makes the accept→patch→accept audit trail — the very loop
// the v0.5.0 fix milestones harden — directly inspectable without jq, and
// gives the §2 versioned-audit-trail primitive a first-class viewer.
func newLogCmd() *cobra.Command {
	var (
		ledgerPath string
		jsonOut    bool
		jsonPretty bool
	)
	c := &cobra.Command{
		Use:   "log",
		Short: "Pretty-print the append-only ledger as a human-readable audit trail.",
		Long: `Read the append-only deviation ledger and pretty-print one line per
LedgerEntry (ts, op, plan_version, step_id, kind, and accepted /
patched_to_version where present) so the accept→patch→rollback audit trail is
inspectable without jq.

Pass --json to emit the LedgerEntry slice as a JSON array for machine consumers.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runLog(ledgerPath, jsonOut, jsonPretty, cmd.OutOrStdout())
		},
	}
	c.Flags().StringVarP(&ledgerPath, "ledger", "l", DefaultLedgerPath, "deviation ledger path")
	c.Flags().BoolVar(&jsonOut, "json", false, "emit the ledger as a JSON array (machine-readable)")
	c.Flags().BoolVar(&jsonPretty, "json-pretty", false, "pretty-print the --json output (indent 2 spaces)")
	return c
}

// runLog reads the ledger JSONL via ledger.Read and pretty-prints one line per
// LedgerEntry in append order (which is the audit order: accept → patch →
// rollback). A missing ledger is a fresh run — it prints a zero-entry summary
// rather than erroring, mirroring ledger.Read's nil/nil contract.
func runLog(ledgerPath string, jsonOut, jsonPretty bool, out io.Writer) error {
	entries, err := ledger.Read(ledgerPath)
	if err != nil {
		return err
	}
	if jsonOut {
		enc := json.NewEncoder(out)
		if jsonPretty {
			enc.SetIndent("", "  ")
		}
		if err := enc.Encode(entries); err != nil {
			return fmt.Errorf("log: encode json: %w", err)
		}
		return nil
	}
	fmt.Fprintf(out, "ledger %s — %d entr%s\n", ledgerPath, len(entries), entryPlural(len(entries)))
	fmt.Fprintln(out, "------")
	for _, e := range entries {
		fmt.Fprintln(out, formatLedgerEntry(e))
	}
	return nil
}

func entryPlural(n int) string {
	if n == 1 {
		return "y"
	}
	return "ies"
}

// formatLedgerEntry renders one LedgerEntry as a single stdout line: ts, op,
// plan_version, step_id, kind, summary, and accepted / patched_to_version
// where present. The patch marker entry (no deviation) renders with a dash
// step/kind so the row stays scannable.
func formatLedgerEntry(e ledger.Entry) string {
	ts := "—"
	if !e.TS.IsZero() {
		ts = e.TS.Format("2006-01-02 15:04:05")
	}
	stepID := e.Deviation.StepID
	if stepID == "" {
		stepID = "—"
	}
	kind := string(e.Deviation.Kind)
	if kind == "" {
		kind = "—"
	}
	row := fmt.Sprintf("%s  %-8s  plan:%-12s  step:%-14s  kind:%-11s  %s",
		ts, e.Op, e.PlanVersion, stepID, kind, e.Deviation.Summary)
	if e.Deviation.Accepted {
		row += "  [accepted]"
	}
	if e.Deviation.PatchedToVersion != "" {
		row += fmt.Sprintf("  patched_to:%s", e.Deviation.PatchedToVersion)
	}
	return row
}
