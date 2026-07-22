// Package plan parses a DriftLedger plan contract from markdown.
//
// A plan contract is the versioned, human-authored checklist an agent promises
// to execute. The markdown schema is intentionally tiny so a developer can write
// it in under a minute:
//
//	# Plan: <title>
//
//	version: 0.1.0
//
//	## step-1
//	intent: Scaffold the project structure
//	accept: go module initialized
//	accept: cmd package present
//
//	## step-2
//	intent: Implement the reconciler
//	accept: matched status
//
// The parser is structural and forgiving: a `## <id>` heading opens a step, an
// `intent:` line sets its intent, and `accept:` lines collect accept criteria.
// An inline intent after the heading (`## step-1: do the thing`) is also honored.
package plan

import (
	"bufio"
	"fmt"
	"os"
	"regexp"
	"strings"
)

// PlanContract is the versioned agreement between a user and their agent.
// It is the static half of the plan-execution-deviation-ledger primitive: the
// agent's live trace (see package trace) is reconciled against it.
type PlanContract struct {
	Version string
	Steps   []Step
}

// Step is one promised unit of work in a plan contract.
type Step struct {
	ID             string
	Intent        string
	AcceptCriteria []string
}

// StepByID returns the step with the given id and a found flag.
func (c *PlanContract) StepByID(id string) (Step, bool) {
	for _, s := range c.Steps {
		if s.ID == id {
			return s, true
		}
	}
	return Step{}, false
}

var (
	versionRe = regexp.MustCompile(`(?i)^version:\s*(\S+)`)
	headingRe = regexp.MustCompile(`^##\s+(\S+)(?:\s*[:\-—]\s*(.+))?$`)
	intentRe  = regexp.MustCompile(`(?i)^intent:\s*(.+)$`)
	acceptRe  = regexp.MustCompile(`(?i)^accept(?:-?criteria)?:\s*(.+)$`)
)

// ParseMarkdown parses a plan contract from markdown text.
//
// The default version is "0.1.0" when no `version:` line is present. Duplicate
// step ids are rejected because the reconciler keys deviations off step id.
func ParseMarkdown(text string) (*PlanContract, error) {
	c := &PlanContract{Version: "0.1.0"}
	seen := make(map[string]bool)
	var current *Step

	sc := bufio.NewScanner(strings.NewReader(text))
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") && !strings.HasPrefix(line, "##") {
			// Skip blank lines and lone `# title` lines. A `# Plan` header is
			// decorative prose, not a step.
			continue
		}

		if m := versionRe.FindStringSubmatch(line); m != nil {
			c.Version = strings.TrimSpace(m[1])
			continue
		}

		if m := headingRe.FindStringSubmatch(line); m != nil {
			id := strings.TrimSpace(m[1])
			if id == "" {
				return nil, fmt.Errorf("plan: step heading without id: %q", line)
			}
			if seen[id] {
				return nil, fmt.Errorf("plan: duplicate step id %q", id)
			}
			seen[id] = true
			step := Step{ID: id}
			if m[2] != "" {
				step.Intent = strings.TrimSpace(m[2])
			}
			c.Steps = append(c.Steps, step)
			current = &c.Steps[len(c.Steps)-1]
			continue
		}

		if current == nil {
			continue
		}
		if m := intentRe.FindStringSubmatch(line); m != nil {
			current.Intent = strings.TrimSpace(m[1])
			continue
		}
		if m := acceptRe.FindStringSubmatch(line); m != nil {
			current.AcceptCriteria = append(current.AcceptCriteria, strings.TrimSpace(m[1]))
			continue
		}
		// Any other line under a heading is freeform prose — ignored.
	}
	if err := sc.Err(); err != nil {
		return nil, fmt.Errorf("plan: scan: %w", err)
	}
	if len(c.Steps) == 0 {
		return nil, fmt.Errorf("plan: no steps found (expect at least one `## <id>` heading)")
	}
	return c, nil
}

// ParseFile reads a markdown file and parses it as a plan contract.
func ParseFile(path string) (*PlanContract, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("plan: read %s: %w", path, err)
	}
	return ParseMarkdown(string(data))
}

// DefaultPlanMarkdown is the example contract written by `driftledger init`.
// It demonstrates the minute-20-vs-88 demo: three steps, the third of which an
// agent tends to abandon mid-flight — exactly what the live ledger catches.
// Accept criteria are written as keyword lists: the structural reconciler
// matches each criterion's keywords (whole words) against the agent's trace
// summaries, so phrase them as the nouns/verbs that must appear when the step
// is done.
const DefaultPlanMarkdown = `# Plan: demo run

version: 0.1.0

## step-1
intent: Scaffold the project structure
accept: go module
accept: cmd package

## step-2
intent: Implement the structural reconciler
accept: matched
accept: drifting
accept: unexecuted

## step-3
intent: Write tests covering the deviation diff
accept: reconcile_test
`
