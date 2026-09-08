#!/usr/bin/env bash
set -euo pipefail
go run ./cmd/driftledger diff examples/presentation-plan.md examples/presentation-trace.jsonl --ledger examples/presentation-empty-ledger.jsonl --json --json-pretty
