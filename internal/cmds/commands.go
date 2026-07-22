// Package cmds wires the cobra CLI surface: init / diff / watch (m1) plus
// patch / rollback stubs that land in m2 / m3.
package cmds

import (
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
JSONL you can inspect with jq. m1 ships diff + watch; patch (m2) and rollback
(m3) are stubbed on the roadmap.`,
		Version: "0.1.0",
	}

	root.AddCommand(newInitCmd())
	root.AddCommand(newDiffCmd())
	root.AddCommand(newWatchCmd())
	root.AddCommand(newPatchCmd())
	root.AddCommand(newRollbackCmd())
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
	var ledgerPath string
	c := &cobra.Command{
		Use:   "diff <plan.md> <trace.jsonl>",
		Short: "Print plan-vs-trace deviations to stdout (non-interactive).",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runDiff(args[0], args[1], ledgerPath, cmd.OutOrStdout())
		},
	}
	c.Flags().StringVarP(&ledgerPath, "ledger", "l", DefaultLedgerPath, "deviation ledger path (accept overlay)")
	return c
}

func runDiff(planPath, tracePath, ledgerPath string, out io.Writer) error {
	p, err := plan.ParseFile(planPath)
	if err != nil {
		return err
	}
	events, err := trace.ParseFile(tracePath)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	devs := diff.Reconcile(p, events)

	accepted, err := ledger.AcceptedStepIDs(ledgerPath)
	if err != nil {
		// A missing ledger is normal for a fresh run — surface nothing, just
		// reconcile without the overlay.
		accepted = nil
	}
	devs = diff.OverlayAccepted(devs, accepted)

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

// --- patch / rollback (m2 / m3 stubs) ------------------------------------

func newPatchCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "patch",
		Short: "(m2 roadmap) rewrite the plan contract to a new version capturing accepted drift.",
		RunE: func(cmd *cobra.Command, args []string) error {
			fmt.Fprintln(cmd.OutOrStdout(),
				"patch is an m2 roadmap item — it will rewrite the plan contract to a new\n"+
					"semantic version capturing accepted deviations and append a `patch` entry to\n"+
					"the ledger, turning the append-only view into a versioned audit trail.")
			return nil
		},
	}
}

func newRollbackCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "rollback",
		Short: "(m3 roadmap) emit a git-revert + checkpoint-tag directive for accepted drift.",
		RunE: func(cmd *cobra.Command, args []string) error {
			fmt.Fprintln(cmd.OutOrStdout(),
				"rollback is an m3 roadmap item — it will emit (never execute) a `git revert`\n"+
					"plus checkpoint-tag directive for accepted deviations, closing the\n"+
					"patch / accept / rollback loop with an asciinema cast for the launch.")
			return nil
		},
	}
}
