# VHS tape — DriftLedger 10-minute happy path.
# Renders assets/demo.gif. Drives the real `driftledger` binary (built by the
# demo.yml workflow via `go build -o /usr/local/bin/driftledger ./cmd/driftledger`).
# The flow: init a 3-step plan, simulate an agent drifting off step-3 at minute
# 20, run `diff` to surface the drift, and peek at the (still-empty) ledger.

Output assets/demo.gif
Set Width 110
Set Height 30
Set FontSize 13
Set Padding 10
Set Theme { "background": "#1D1D1F" }

# 1. Scaffold the versioned plan contract.
Type "driftledger init -p plan.md"
Enter
Sleep 800ms

# 2. Show the contract: three steps, the third is where drift will appear.
Type "sed -n '3,40p' plan.md"
Enter
Sleep 800ms

# 3. Simulate the agent's trace: step-1 done, step-2 half-done, step-3 abandoned
#    for a README tangent at minute 20.
Type "cat > trace.jsonl <<'EOF'"
Enter
Type '{"ts":"2026-07-23T10:00:00Z","step_id":"step-1","action":"run","summary":"initialized go module and added cmd package"}'
Enter
Type '{"ts":"2026-07-23T10:20:00Z","step_id":"step-2","action":"run","summary":"started a reconciler, punted on statuses"}'
Enter
Type '{"ts":"2026-07-23T10:20:30Z","step_id":"side-quest","action":"run","summary":"rewrote the README hero instead"}'
Enter
Type 'EOF'
Enter
Sleep 600ms

# 4. Surface the drift — the minute-20-vs-88 moment, non-interactively.
Type "driftledger diff plan.md trace.jsonl"
Enter
Sleep 1s

# 5. The ledger is jq-inspectable; nothing accepted yet.
Type "jq . driftledger.ledger.jsonl 2>/dev/null || echo '(no accepts yet — run driftledger watch and press a)'"
Enter
Sleep 1s
