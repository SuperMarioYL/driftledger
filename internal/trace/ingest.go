// Package trace ingests the JSONL execution stream an agent emits while it runs.
//
// The trace is the dynamic half of the plan-execution-deviation-ledger primitive:
// one JSON object per line, appended live as the agent acts. DriftLedger defines
// a single, minimal schema (an agent shim wraps the agent's commands to append
// lines to it):
//
//	{"ts":"2026-07-23T10:00:00Z","step_id":"step-1","action":"run","summary":"go mod init github.com/SuperMarioYL/driftledger"}
//
// `step_id` is optional — a line without one still reconciles as `extra` work.
package trace

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"time"
)

// Event is one line of the agent's JSONL execution trace.
type Event struct {
	TS      time.Time `json:"ts"`
	StepID  string    `json:"step_id,omitempty"`
	Action  string    `json:"action"`
	Summary string    `json:"summary"`
}

// ParseLine parses a single JSONL trace line. Blank lines are skipped (returns
// io.EOF so callers can treat trailing newlines as "no event").
func ParseLine(line string) (Event, error) {
	trimmed := trimSpace(line)
	if trimmed == "" {
		return Event{}, io.EOF
	}
	var ev Event
	if err := json.Unmarshal([]byte(trimmed), &ev); err != nil {
		return Event{}, fmt.Errorf("trace: parse line: %w", err)
	}
	if ev.Action == "" {
		return Event{}, fmt.Errorf("trace: event missing action: %s", trimmed)
	}
	if ev.TS.IsZero() {
		// An agent shim that omits ts still reconciles; stamp "now" so the
		// first-seen-ts in deviations stays monotonic.
		ev.TS = time.Now().UTC()
	}
	return ev, nil
}

// ParseReader reads every JSONL line from r and returns the events in order.
// Malformed lines are skipped with a count returned alongside the events so a
// caller can surface "3 unparseable lines" without aborting the whole stream.
func ParseReader(r io.Reader) ([]Event, int, error) {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	var events []Event
	skipped := 0
	for sc.Scan() {
		ev, err := ParseLine(sc.Text())
		if err == io.EOF {
			continue
		}
		if err != nil {
			skipped++
			continue
		}
		events = append(events, ev)
	}
	if err := sc.Err(); err != nil {
		return events, skipped, fmt.Errorf("trace: scan: %w", err)
	}
	return events, skipped, nil
}

// ParseFile reads a whole JSONL trace file. Missing files return nil + error.
func ParseFile(path string) ([]Event, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("trace: open %s: %w", path, err)
	}
	defer f.Close()
	events, _, err := ParseReader(f)
	return events, err
}

// trimSpace strips ASCII whitespace (the JSON lines come from a shim that may
// add trailing \r on Windows-style newlines).
func trimSpace(s string) string {
	for len(s) > 0 && (s[0] == ' ' || s[0] == '\t' || s[0] == '\r' || s[0] == '\n') {
		s = s[1:]
	}
	for len(s) > 0 {
		c := s[len(s)-1]
		if c == ' ' || c == '\t' || c == '\r' || c == '\n' {
			s = s[:len(s)-1]
			continue
		}
		break
	}
	return s
}
