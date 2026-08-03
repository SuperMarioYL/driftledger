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

# json_escape prints its first arg as a JSON-escaped string (RFC 8259). Defined
# before the call site — bash registers a function only once the definition is
# reached top-to-bottom. LC_ALL=C forces byte-wise indexing so a multi-byte
# UTF-8 summary is not split mid-codepoint; UTF-8 lead/continuation bytes are
# all >= 0x80 and pass through as raw UTF-8 (valid in JSON). Pure bash, no
# jq / python3 — preserves the single-binary, no-deps pitch.
json_escape() {
  local LC_ALL=C s=$1 ch out=
  local i=0 code
  while (( i < ${#s} )); do
    ch=${s:i:1}
    case "$ch" in
      \\)    out+='\\' ;;    # \  -> \\
      \")    out+='\"' ;;    # "  -> \"
      $'\n') out+='\n' ;;    # newline -> \n
      $'\t') out+='\t' ;;    # tab     -> \t
      $'\r') out+='\r' ;;    # CR      -> \r
      $'\b') out+='\b' ;;    # backspace -> \b
      $'\f') out+='\f' ;;    # formfeed  -> \f
      *)
        # printf '%d' "'<char>" is the bash ord() trick: the leading single
        # quote tells printf to take the numeric value of the next char.
        printf -v code '%d' "'$ch"
        if (( code < 0x20 )); then
          printf -v code '\\u%04x' "$code"
          out+="$code"
        else
          out+="$ch"
        fi ;;
    esac
    i=$((i+1))
  done
  printf '%s' "$out"
}

step_id="${1:?usage: trace-shim.sh <step_id> <summary> -- <command...>}"
summary="${2:?usage: trace-shim.sh <step_id> <summary> -- <command...>}"
shift 2
if [[ "${1:-}" != "--" ]]; then
  echo "trace-shim: expected '--' before the command" >&2
  exit 2
fi
shift

ts=$(date -u +"%Y-%m-%dT%H:%M:%SZ")
# JSON-escape step_id AND summary so a control char (newline / tab / CR / etc.)
# in either field does not produce invalid JSON that trace.ParseLine silently
# skips — which would drop the agent's step event from the trace and falsify
# the deviation ledger.
esc_step_id=$(json_escape "$step_id")
esc_summary=$(json_escape "$summary")
printf '{"ts":"%s","step_id":"%s","action":"run","summary":"%s"}\n' \
  "$ts" "$esc_step_id" "$esc_summary" >> "${DRIFTLEDGER_TRACE:-trace.jsonl}"

exec "$@"
