# Changelog

All notable changes to DriftLedger are documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

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
