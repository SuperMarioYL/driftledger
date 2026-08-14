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
	"strconv"
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

// AcceptedFold is one accepted deviation to fold into a patched plan contract.
// It is a plain-shape mirror of diff.Deviation so the plan package does not
// import diff (which would create an import cycle — diff already imports plan).
type AcceptedFold struct {
	StepID  string
	Kind    string
	Summary string
}

// BumpVersion increments the patch component of a semantic version string
// ("0.1.0" -> "0.1.1", "v0.1.0" -> "0.1.2"). Each accepted-deviation fold is a
// small contract revision, so the patch component is the conservative bump;
// repeated patches accumulate (0.1.1, 0.1.2, ...). A version that is not three
// numeric dot-separated components falls back to "<v>.patch1" so the bump is
// always observable rather than silently dropping a malformed version.
//
// The result is always BARE semver (no leading "v"): callers that print a
// version to a user (e.g. runPatch's "patched %s -> v%s" and the accepted
// annotation's "folded into v%s") prepend exactly one "v". Preserving a leading
// "v" here would make those callers emit "vv0.3.1" for a v-prefixed plan
// version (v0.4.0 fix-patch-version-double-v).
func BumpVersion(v string) string {
	v = strings.TrimSpace(v)
	if v == "" {
		v = "0.0.0"
	}
	v = strings.TrimPrefix(v, "v")
	parts := strings.Split(v, ".")
	if len(parts) == 3 {
		if n, err := strconv.Atoi(parts[2]); err == nil {
			return parts[0] + "." + parts[1] + "." + strconv.Itoa(n+1)
		}
	}
	return v + ".patch1"
}

// ApplyPatchMarkdown rewrites a plan markdown document to a new semantic version
// and folds each accepted deviation into the contract as an `accepted:`
// annotation under its step's heading. The annotation is freeform prose to the
// parser (it does not match the `accept:` regex), so the rewritten plan parses
// cleanly while a human reader sees which steps' drift was folded into the new
// contract version. If the source has no `version:` line, one is appended so
// the rewritten contract carries the new version (the parser finds it anywhere).
//
// This realizes the base plan's m2_patch_contract milestone: drift accepted
// mid-flight becomes a first-class plan revision, and the ledger (which records
// the matching patch entry) becomes a versioned audit trail.
func ApplyPatchMarkdown(md, newVersion string, folds []AcceptedFold) (string, error) {
	byStep := make(map[string]AcceptedFold, len(folds))
	for _, f := range folds {
		byStep[f.StepID] = f
	}
	sc := bufio.NewScanner(strings.NewReader(md))
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	var out strings.Builder
	versionBumped := false
	for sc.Scan() {
		line := sc.Text()
		if versionRe.MatchString(line) {
			out.WriteString("version: ")
			out.WriteString(newVersion)
			out.WriteByte('\n')
			versionBumped = true
			continue
		}
		out.WriteString(line)
		out.WriteByte('\n')
		if m := headingRe.FindStringSubmatch(line); m != nil {
			if fold, ok := byStep[strings.TrimSpace(m[1])]; ok {
				writeAcceptedAnnotation(&out, fold, newVersion)
			}
		}
	}
	if err := sc.Err(); err != nil {
		return "", fmt.Errorf("plan: patch scan: %w", err)
	}
	if !versionBumped {
		out.WriteString("version: ")
		out.WriteString(newVersion)
		out.WriteByte('\n')
	}
	return out.String(), nil
}

// writeAcceptedAnnotation appends one `accepted:` line under a step heading.
// The line is freeform prose to the parser (it does not match `accept:`) and is
// kept on a single line: a trace summary can decode to a real newline, which
// would otherwise split the annotation across plan lines.
func writeAcceptedAnnotation(out *strings.Builder, fold AcceptedFold, newVersion string) {
	kind := fold.Kind
	if kind == "" {
		kind = "deviation"
	}
	note := strings.ReplaceAll(fold.Summary, "\n", " ")
	note = strings.ReplaceAll(note, "\r", "")
	if note != "" {
		fmt.Fprintf(out, "accepted: %s folded into v%s (%s)\n", kind, newVersion, note)
	} else {
		fmt.Fprintf(out, "accepted: %s folded into v%s\n", kind, newVersion)
	}
}
