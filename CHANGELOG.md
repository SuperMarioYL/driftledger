# Changelog

All notable changes to DriftLedger are documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [0.8.0] - 2026-09-04

A maintenance release that moves all three release-semver surfaces in
lockstep and adds a single-source-of-truth guard so a future bump cannot
silently leave one surface behind. No reconcile, plan/trace parsing, ledger,
or feature surface changed.

### Fixed

- **Add a single-source-of-truth version test guarding surface drift.** The
  release version lived on three surfaces — the `VERSION` file, the cobra root
  command's `Version` field (the `driftledger --version` output), and the
  `CHANGELOG.md` head entry — with no test asserting they agree, so a bump
  that touched only one shipped silently out of sync (the v0.6.0 changelog
  itself documents a prior "VERSION stale at 0.4.0" lag). A new
  `internal/cmds/version_test.go` reads the `VERSION` file, the root
  command's `Version` field, and the first `## [x.y.z]` heading from
  `CHANGELOG.md`, and asserts all three equal the shipped version. The test
  fails on a tag whose surfaces disagree, so a single-surface miss is caught
  before release.
  (`internal/cmds/version_test.go`)

### Changed

- Bumped the `VERSION` file and the `driftledger --version` surface to `0.8.0`.

[0.8.0]: https://github.com/SuperMarioYL/driftledger/releases/tag/v0.8.0

## [0.7.0] - 2026-08-25

Three in-lane correctness fixes that close the silent-failure and
atomicity-permission gaps the prior versions targeted. No reconcile, plan/trace
parsing, or feature surface changed.

### Fixed

- **Stop `driftledger diff` from erroring on a missing trace file.** `runDiff`
  guarded the trace-read error with `os.IsNotExist`, but `trace.ParseFile` wraps
  `os.Open`'s not-exist error with `fmt.Errorf("trace: open %s: %w"…)` and
  `os.IsNotExist` does NOT unwrap a `%w`-wrapped error (`errors.Is` does), so the
  NotExist case never matched and `diff` returned the error instead of
  reconciling an empty (all-unexecuted) trace — the common start-of-run /
  pre-trace CI case. The guard now uses `errors.Is(err, os.ErrNotExist)`,
  mirroring the watch TUI path (`fix-tui-refresh-wipes-on-trace-error`), so a
  missing trace is swallowed and `diff.Reconcile(plan, nil)` paints every step
  unexecuted.
  (`internal/cmds/commands.go`)

- **Surface malformed ledger lines instead of silently reconciling a partial
  ledger.** `ledger.Read` did `if err := json.Unmarshal(...); err != nil {
  continue }` — silently skipping any malformed JSONL line with NO skipped count
  returned (signature `([]Entry, error)`), so every caller silently computed a
  partial ledger and a corrupted accept line silently dropped that accept.
  `Read` now returns a skipped count (mirroring `trace.ParseReader`), surfaced
  by `diff` (stderr), `log` (a summary line / stderr under `--json`), `patch` /
  `rollback` (stderr), and the watch TUI's `overlayAccepted` (the `m.err` band)
  so a partial ledger is never silently reconciled.
  (`internal/ledger/ledger.go`, `internal/cmds/commands.go`, `internal/tui/app.go`)

- **Preserve the plan file's permissions across a patch.** `runPatch` writes the
  rewritten plan via `os.CreateTemp` (mode 0o600) then `os.Rename`s it onto the
  plan path, replacing the plan's inode, so a plan authored 0o644 (the mode
  `driftledger init` writes) became 0o600 after the first patch — silently
  stripping group/other read so a downstream CI step or teammate reading
  `plan.md` as a different user failed. `runPatch` now stats the plan's mode
  before the rewrite and `os.Chmod`s the temp file to it before the rename so the
  rewritten plan keeps the user's original permissions.
  (`internal/cmds/commands.go`)

### Changed

- Bumped the `VERSION` file and the `driftledger --version` surface to `0.7.0`.

[0.7.0]: https://github.com/SuperMarioYL/driftledger/releases/tag/v0.7.0

## [0.6.0] - 2026-08-21

Two in-lane correctness fixes that stop the ledger-read and
`driftledger log --json` paths from silently swallowing errors. No reconcile
semantics, plan/trace parsing, or feature surface changed.

### Fixed

- **Surface non-`NotExist` ledger-read errors in `diff`/`watch` instead of
  swallowing them.** `runDiff` called `ledger.AcceptedStepIDs` and on *any* error
  set `accepted = nil` and continued, although the surrounding comment only
  justified the missing-file case. A permission/IO open failure or a `bufio`
  scan error (a ledger line exceeding the 1 MB scanner buffer) silently dropped
  the accept overlay, so previously-accepted drift showed as unaccepted — and
  under `--fail-on-drift` (the v0.5.0 CI gate) the build failed spuriously. The
  same swallow in the watch TUI's `overlayAccepted` returned early without
  setting `m.err`, so the live ledger silently lost accepted state with no error
  band. Now only the `os.IsNotExist`/`os.ErrNotExist` case is swallowed (a missing
  ledger is normal for a fresh run); every other read error is surfaced —
  `runDiff` returns it, `overlayAccepted` sets `m.err`. This mirrors the v0.5.0
  trace-read guard (`fix-tui-refresh-wipes-on-trace-error`), now extended to the
  ledger-read path.
  (`internal/cmds/commands.go`, `internal/tui/app.go`)

- **`driftledger log --json` emits `[]` not `null` for an empty/missing ledger.**
  `runLog` encoded `ledger.Read`'s result, a nil `[]Entry` for a missing/empty
  ledger; `json.Marshal` of a nil slice emits the literal `null`, not `[]`. The
  `--json` flag help promises "emit the ledger as a JSON array", so this broke
  naive consumers (e.g. Python `for e in json.loads(stdout)` crashes on `null`
  where it expects a list). The nil slice is now normalized to an empty non-nil
  slice before encoding so `log --json` always emits a JSON array.
  (`internal/cmds/commands.go`)

### Changed

- Bumped the `VERSION` file (stale at `0.4.0`, a v0.5.0 lag) straight to `0.6.0`,
  and bumped the `driftledger --version` surface to `0.6.0`.

[0.6.0]: https://github.com/SuperMarioYL/driftledger/releases/tag/v0.6.0
