#!/usr/bin/env bash
# A thin trace shim: wrap any command so it appends one JSONL line to
# trace.jsonl before/after it runs. Point DriftLedger at the same plan + this
# trace file to watch your agent drift live.
#
#   trace-shim.sh step-1 "go mod init github.com/me/x" -- go mod init github.com/me/x
#
# Usage:
#   trace-shim.sh <step_id> <summary> -- <command...>
set -euo pipefail

step_id="${1:?usage: trace-shim.sh <step_id> <summary> -- <command...>}"
summary="${2:?usage: trace-shim.sh <step_id> <summary> -- <command...>}"
shift 2
if [[ "${1:-}" != "--" ]]; then
  echo "trace-shim: expected '--' before the command" >&2
  exit 2
fi
shift

ts=$(date -u +"%Y-%m-%dT%H:%M:%SZ")
# JSON-escape the summary naively (good enough for a dev shim).
esc_summary=${summary//\\/\\\\}; esc_summary=${esc_summary//\"/\\\"}
printf '{"ts":"%s","step_id":"%s","action":"run","summary":"%s"}\n' \
  "$ts" "$step_id" "$esc_summary" >> "${DRIFTLEDGER_TRACE:-trace.jsonl}"

exec "$@"
