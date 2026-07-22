package plan

import (
	"os"
	"path/filepath"
	"testing"
)

const samplePlan = `# Plan: demo run

version: 0.2.0

## step-1
intent: Scaffold the project structure
accept: go module initialized
accept: cmd package present

## step-2
intent: Implement the reconciler

## step-3: Write tests covering the deviation diff
accept: reconcile_test passes
`

func TestParseMarkdown(t *testing.T) {
	c, err := ParseMarkdown(samplePlan)
	if err != nil {
		t.Fatalf("ParseMarkdown: %v", err)
	}
	if c.Version != "0.2.0" {
		t.Errorf("version = %q, want 0.2.0", c.Version)
	}
	if len(c.Steps) != 3 {
		t.Fatalf("steps = %d, want 3", len(c.Steps))
	}
	if c.Steps[0].ID != "step-1" || c.Steps[0].Intent != "Scaffold the project structure" {
		t.Errorf("step-1 unexpected: %+v", c.Steps[0])
	}
	if len(c.Steps[0].AcceptCriteria) != 2 {
		t.Errorf("step-1 accept count = %d, want 2", len(c.Steps[0].AcceptCriteria))
	}
	// step-2 has no accept criteria.
	if len(c.Steps[1].AcceptCriteria) != 0 {
		t.Errorf("step-2 accept count = %d, want 0", len(c.Steps[1].AcceptCriteria))
	}
	// step-3 uses the inline intent form (`## step-3: Write tests...`).
	if c.Steps[2].ID != "step-3" {
		t.Errorf("step-3 id = %q", c.Steps[2].ID)
	}
	if c.Steps[2].Intent != "Write tests covering the deviation diff" {
		t.Errorf("step-3 inline intent = %q", c.Steps[2].Intent)
	}
}

func TestParseMarkdownDefaultVersion(t *testing.T) {
	c, err := ParseMarkdown("## s1\nintent: do it\n")
	if err != nil {
		t.Fatalf("ParseMarkdown: %v", err)
	}
	if c.Version != "0.1.0" {
		t.Errorf("default version = %q, want 0.1.0", c.Version)
	}
}

func TestParseMarkdownDuplicateStepID(t *testing.T) {
	_, err := ParseMarkdown("## s1\nintent: a\n## s1\nintent: b\n")
	if err == nil {
		t.Fatal("expected error for duplicate step id, got nil")
	}
}

func TestParseMarkdownNoSteps(t *testing.T) {
	if _, err := ParseMarkdown("# Plan\nversion: 0.1.0\n"); err == nil {
		t.Fatal("expected error when no steps present, got nil")
	}
}

func TestStepByID(t *testing.T) {
	c, _ := ParseMarkdown(samplePlan)
	s, ok := c.StepByID("step-2")
	if !ok {
		t.Fatal("step-2 not found")
	}
	if s.Intent != "Implement the reconciler" {
		t.Errorf("step-2 intent = %q", s.Intent)
	}
	if _, ok := c.StepByID("missing"); ok {
		t.Error("missing step should not be found")
	}
}

func TestParseFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "plan.md")
	if err := os.WriteFile(path, []byte(samplePlan), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	c, err := ParseFile(path)
	if err != nil {
		t.Fatalf("ParseFile: %v", err)
	}
	if len(c.Steps) != 3 {
		t.Fatalf("steps = %d, want 3", len(c.Steps))
	}
}

func TestParseFileMissing(t *testing.T) {
	if _, err := ParseFile("/nonexistent/plan.md"); err == nil {
		t.Fatal("expected error for missing file, got nil")
	}
}

func TestDefaultPlanMarkdownParses(t *testing.T) {
	// The contract shipped by `driftledger init` must itself parse cleanly.
	c, err := ParseMarkdown(DefaultPlanMarkdown)
	if err != nil {
		t.Fatalf("DefaultPlanMarkdown did not parse: %v", err)
	}
	if len(c.Steps) != 3 {
		t.Fatalf("default plan steps = %d, want 3", len(c.Steps))
	}
	if c.Version != "0.1.0" {
		t.Errorf("default plan version = %q, want 0.1.0", c.Version)
	}
}
