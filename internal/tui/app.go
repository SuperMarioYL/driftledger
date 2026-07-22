// Package tui renders the live deviation ledger in a bubbletea TUI.
//
// `driftledger watch plan.md trace.jsonl` opens this view in the tmux pane next
// to a coding/research agent. It tails the trace file, re-reconciles on every new
// line, and renders one row per plan step (plus an "extra" section for work the
// agent did outside the contract). The star-earning control is `a` — accept a
// drifting step mid-flight, which appends an entry to the deviation ledger so
// the drift becomes a first-class record instead of a post-mortem discovery.
package tui

import (
	"fmt"
	"os"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/SuperMarioYL/driftledger/internal/diff"
	"github.com/SuperMarioYL/driftledger/internal/ledger"
	"github.com/SuperMarioYL/driftledger/internal/plan"
	"github.com/SuperMarioYL/driftledger/internal/trace"
)

// tickInterval is how often the trace file is re-read for new lines. Polling
// mtime is the simplest portable tail — no inotify/fanotify, no platform split.
const tickInterval = 500 * time.Millisecond

// tickMsg signals the model to refresh the deviation set from the trace file.
type tickMsg time.Time

// Model is the bubbletea model for the live deviation ledger.
type Model struct {
	plan     *plan.PlanContract
	planPath string

	tracePath  string
	traceSize  int64 // last-seen trace file size; growth triggers a re-reconcile
	deviations []diff.Deviation

	ledger *ledger.Ledger
	cursor int

	err     string
	quitting bool
}

// style is the per-kind colour map. Domain = AI/agent → purple+teal per house
// style §1; statuses reuse that palette (teal=matched, amber=drifting,
// slate=unexecuted, purple=extra).
var (
	selStyle = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#0071E3"))

	kindStyle = map[diff.Kind]lipgloss.Style{
		diff.KindMatched:    lipgloss.NewStyle().Foreground(lipgloss.Color("#10A37F")),
		diff.KindDrifting:   lipgloss.NewStyle().Foreground(lipgloss.Color("#D97706")),
		diff.KindUnexecuted: lipgloss.NewStyle().Foreground(lipgloss.Color("#6E6E73")),
		diff.KindExtra:      lipgloss.NewStyle().Foreground(lipgloss.Color("#5E5CE6")),
	}
	dimStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("#6E6E73"))
	accStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("#10A37F"))
)

// New loads the plan contract and any prior accepted state, returning a model
// ready for a tea.Program. A missing trace file is fine — the ledger paints
// every step as `unexecuted` until the agent starts appending events.
func New(planPath, tracePath, ledgerPath string) (Model, error) {
	p, err := plan.ParseFile(planPath)
	if err != nil {
		return Model{}, err
	}
	m := Model{
		plan:      p,
		planPath:  planPath,
		tracePath: tracePath,
		ledger:    ledger.New(ledgerPath),
	}
	m.refresh()
	return m, nil
}

// Run starts the watch TUI as a blocking bubbletea program. It is the
// `driftledger watch` entry point; tests exercise New/Update/View directly
// rather than spinning a real terminal.
func Run(planPath, tracePath, ledgerPath string) error {
	m, err := New(planPath, tracePath, ledgerPath)
	if err != nil {
		return err
	}
	p := tea.NewProgram(m, tea.WithAltScreen())
	_, _ = p.Run()
	return nil
}

// Init kicks off the first tick so the ledger repaints as the trace grows.
func (m Model) Init() tea.Cmd {
	return tick()
}

// Update handles ticks (re-reconcile) and keys (navigate / accept / quit).
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tickMsg:
		m.refresh()
		if m.quitting {
			return m, nil
		}
		return m, tick()

	case tea.KeyMsg:
		switch msg.Type {
		case tea.KeyCtrlC, tea.KeyEsc:
			m.quitting = true
			return m, tea.Quit
		case tea.KeyUp:
			if m.cursor > 0 {
				m.cursor--
			}
		case tea.KeyDown:
			if m.cursor < len(m.deviations)-1 {
				m.cursor++
			}
		case tea.KeyRunes:
			switch string(msg.Runes) {
			case "q":
				m.quitting = true
				return m, tea.Quit
			case "a":
				m.acceptSelected()
			case "j":
				if m.cursor < len(m.deviations)-1 {
					m.cursor++
				}
			case "k":
				if m.cursor > 0 {
					m.cursor--
				}
			}
		}
	}
	return m, nil
}

// View renders the deviation ledger.
func (m Model) View() string {
	var b strings.Builder
	fmt.Fprintf(&b, "DriftLedger  plan %s  (%d steps)\n", m.plan.Version, len(m.plan.Steps))
	fmt.Fprintf(&b, "trace: %s\n\n", m.displayTracePath())

	if len(m.deviations) == 0 {
		b.WriteString("no steps in plan — nothing to reconcile yet.\n")
	} else {
		b.WriteString(pad("  STEP", 16))
		b.WriteString(pad("STATUS", 12))
		b.WriteString(pad("FIRST SEEN", 22))
		b.WriteString("DETAIL\n")
		for i, d := range m.deviations {
			row := m.renderRow(d, i == m.cursor)
			b.WriteString(row)
			b.WriteByte('\n')
		}
	}

	fmt.Fprintf(&b, "\n%s\n", m.renderLegend())
	if m.err != "" {
		fmt.Fprintf(&b, "\n%s\n", dimStyle.Render(m.err))
	}
	return b.String()
}

// refresh re-reads the trace file (only if it grew), re-reconciles, and overlays
// accepted state from the ledger. It is safe to call from Update and from tests.
func (m *Model) refresh() {
	size, err := fileSize(m.tracePath)
	if err == nil && size == m.traceSize && m.deviations != nil {
		// File unchanged since last refresh — still re-overlay accepted in case
		// an accept just landed, but skip the heavier parse+reconcile.
		m.overlayAccepted()
		return
	}
	if err == nil {
		m.traceSize = size
	}
	events, err := trace.ParseFile(m.tracePath)
	if err != nil && !os.IsNotExist(err) {
		m.err = fmt.Sprintf("trace read: %v", err)
	}
	m.deviations = diff.Reconcile(m.plan, events)
	m.overlayAccepted()
	m.err = ""
}

func (m *Model) overlayAccepted() {
	accepted, err := ledger.AcceptedStepIDs(m.ledger.Path())
	if err != nil {
		return
	}
	m.deviations = diff.OverlayAccepted(m.deviations, accepted)
}

// acceptSelected appends an `accept` entry for the deviation under the cursor.
func (m *Model) acceptSelected() {
	if m.cursor < 0 || m.cursor >= len(m.deviations) {
		return
	}
	d := m.deviations[m.cursor]
	if d.Kind == diff.KindExtra {
		// Acceptance keys off plan step id; extras are transient tangents.
		return
	}
	if d.Accepted {
		return
	}
	if err := m.ledger.Accept(m.plan.Version, d); err != nil {
		m.err = fmt.Sprintf("ledger accept: %v", err)
		return
	}
	m.deviations[m.cursor].Accepted = true
}

func (m Model) renderRow(d diff.Deviation, selected bool) string {
	marker := " "
	if selected {
		marker = ">"
	}
	id := d.StepID
	if id == "" {
		id = "(unplanned)"
	}
	styled := kindStyle[d.Kind].Render(string(d.Kind))
	ts := "—"
	if !d.FirstSeenTS.IsZero() {
		ts = d.FirstSeenTS.Format("2006-01-02 15:04:05")
	}
	detail := m.renderDetail(d)
	row := fmt.Sprintf("%s%s%s%s%s",
		marker+" ",
		pad(id, 14),
		pad(styled, 20),
		pad(ts, 22),
		detail,
	)
	if selected {
		row = selStyle.Render(row)
	}
	return row
}

func (m Model) renderDetail(d diff.Deviation) string {
	switch d.Kind {
	case diff.KindDrifting:
		if len(d.UnmetCriteria) > 0 {
			return "unmet: " + strings.Join(d.UnmetCriteria, "; ")
		}
		return "criteria unsatisfied"
	case diff.KindExtra:
		return "tangent: " + d.Summary
	case diff.KindUnexecuted:
		return "no trace events for this step"
	default:
		return ""
	}
}

func (m Model) renderLegend() string {
	return dimStyle.Render("↑/↓ navigate   a accept drift   q quit")
}

func (m Model) displayTracePath() string {
	if m.tracePath == "" {
		return "(none)"
	}
	return m.tracePath
}

func tick() tea.Cmd {
	return tea.Tick(tickInterval, func(t time.Time) tea.Msg {
		return tickMsg(t)
	})
}

// pad right-pads s to width, accounting for lipgloss styling by measuring the
// visible (unstyled) length.
func pad(s string, width int) string {
	visible := lipgloss.Width(s)
	if visible >= width {
		return s + " "
	}
	return s + strings.Repeat(" ", width-visible)
}

func fileSize(path string) (int64, error) {
	fi, err := os.Stat(path)
	if err != nil {
		return 0, err
	}
	return fi.Size(), nil
}
