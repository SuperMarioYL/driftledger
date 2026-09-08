[简体中文](README.zh-CN.md) | **English**

<picture>
  <source media="(max-width: 640px) and (prefers-color-scheme: dark)" srcset="assets/presentation/hero-mobile-dark.svg">
  <source media="(max-width: 640px)" srcset="assets/presentation/hero-mobile-light.svg">
  <source media="(prefers-color-scheme: dark)" srcset="assets/presentation/hero-dark.svg">
  <img src="assets/presentation/hero-light.svg" width="960" alt="DriftLedger — Review the plan beside the execution trace.">
</picture>

**DriftLedger compares Markdown plans with JSONL execution traces, identifies matched, drifting, unexecuted and extra steps, and records human acceptance and plan revisions.**

`Go 1.24+` · [MIT](LICENSE) · [GitHub](https://github.com/SuperMarioYL/driftledger) · [Website](https://driftledger.lei6393.com)

## Why it helps

Plans and actual execution often live in separate places, leaving review focused on the final artifact. Comparing step IDs and acceptance keywords helps identify promises absent from the trace before a human decides whether the deviation is justified. Matching is structural, not a model judgment of completion quality.

<picture>
  <source media="(max-width: 640px) and (prefers-color-scheme: dark)" srcset="assets/presentation/process-mobile-dark.svg">
  <source media="(max-width: 640px)" srcset="assets/presentation/process-mobile-light.svg">
  <source media="(prefers-color-scheme: dark)" srcset="assets/presentation/process-dark.svg">
  <img src="assets/presentation/process-light.svg" width="960" alt="Inspect four distinct deviation kinds">
</picture>

## Architecture

plan parses the version, steps and accept lines; trace reads timestamped events. Reconcile groups by step and compares summary keywords, then ledger overlays human acceptance. diff emits text or JSON, watch provides the review TUI, patch rewrites the plan version, and rollback emits Git instructions for manual handling.

<picture>
  <source media="(max-width: 640px) and (prefers-color-scheme: dark)" srcset="assets/presentation/architecture-mobile-dark.svg">
  <source media="(max-width: 640px)" srcset="assets/presentation/architecture-mobile-light.svg">
  <source media="(prefers-color-scheme: dark)" srcset="assets/presentation/architecture-dark.svg">
  <img src="assets/presentation/architecture-light.svg" width="960" alt="Two inputs and an acceptance ledger">
</picture>

## Install

Requires Go 1.24+. Initial builds may download dependencies; reconciliation calls no model or network service.

```bash
git clone https://github.com/SuperMarioYL/driftledger.git
cd driftledger
go build -o driftledger ./cmd/driftledger
```

## Quickstart

```bash
bash docs/demo.sh
```

The repository supplies a three-step plan, three constructed events with fixed timestamps, and an empty ledger. JSON returns four rows: parse is matched, report is drifting because report json is unmet, verify is unexecuted, and extra is outside the plan. The command changes neither the plan nor Git history.

## Usage

```bash
./driftledger diff examples/presentation-plan.md examples/presentation-trace.jsonl --ledger examples/presentation-empty-ledger.jsonl
./driftledger diff examples/presentation-plan.md examples/presentation-trace.jsonl --json --fail-on-drift
./driftledger watch plan.md trace.jsonl
./driftledger log --json
```

The a key in watch records acceptance. After review, patch plan.md folds pending accepted deviations into a new version and writes both the plan and ledger. rollback plan.md emits commented instructions with a placeholder commit-sha and records the action; it neither executes git revert nor identifies the correct commit automatically.

## Capabilities and integrations

| Interface | Contract |
|---|---|
| Plan | version, ## step-id, intent and accept lines |
| Trace | ts, step_id, action and summary per line |
| Ledger | Appended accept/patch/rollback JSONL |
| diff --json | Machine-readable deviation list |
| --fail-on-drift | Nonzero for unaccepted drifting/unexecuted/extra rows |
| trace-shim.sh | Append events around caller commands; integrate explicitly |

<picture>
  <source media="(max-width: 640px) and (prefers-color-scheme: dark)" srcset="assets/presentation/integrations-mobile-dark.svg">
  <source media="(max-width: 640px)" srcset="assets/presentation/integrations-mobile-light.svg">
  <source media="(prefers-color-scheme: dark)" srcset="assets/presentation/integrations-dark.svg">
  <img src="assets/presentation/integrations-light.svg" width="960" alt="Contracts and review outputs">
</picture>

## Configuration and limits

The default ledger is driftledger.ledger.jsonl in the current directory; --ledger overrides it. Duplicate step IDs are rejected and events without step_id become extra. Malformed trace lines are skipped with warnings, so inspect results derived from partial input.

accept compares tokens formed from ASCII letters, digits, hyphens and underscores, dropping tokens shorter than three characters and stopwords. It understands neither semantics nor negation. An all-Chinese criterion can have no significant tokens and be satisfied when the step has events. Use meaningful machine-checkable keywords and independently verify artifacts.

## Recorded demo

Actual structural comparison output from v0.8.0. Inputs are constructed records, not a real agent run or proof of actual tool actions.

[Inputs, commands and complete output](docs/demo-results.json)

[Retained terminal recording](assets/demo.gif) · [Recording script](docs/demo.tape). The replayable record above describes this example.

## Roadmap

- [x] Plan/trace parsing and four structural deviation kinds.
- [x] Text/JSON output, TUI acceptance and ledger inspection.
- [x] patch revisions and recorded rollback instructions.
- [ ] More native agent trace integrations.
- [ ] Optional semantic judgment and alert integrations.

Automatic Git rollback and guarantees of plan completion quality are not provided.

## Development and license

```bash
go test ./...
```

See [reconcile.go](internal/diff/reconcile.go) for rules and internal/plan plus internal/trace for input formats.

[MIT](LICENSE) · [Issues](https://github.com/SuperMarioYL/driftledger/issues)
