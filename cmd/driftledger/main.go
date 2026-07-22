// Command driftledger is the plan-execution-deviation-ledger CLI.
//
// Build/run:
//
//	go build -o driftledger ./cmd/driftledger
//	driftledger init
//	driftledger diff plan.md trace.jsonl
//	driftledger watch plan.md trace.jsonl
package main

import "github.com/SuperMarioYL/driftledger/internal/cmds"

func main() {
	cmds.Execute()
}
