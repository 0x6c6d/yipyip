#!/usr/bin/env bash
# Verification for T201 (supervisor-owned; workers never edit it). Exit 0 = pass.
. "$(dirname "$0")/../_lib.sh"
. "$(dirname "$0")/_probe.sh"
expect_ok "tests" <<'CMD'
go_tests ./internal/tools TestRegistryPanicRecoveredLogged
go_tests ./internal/agent TestTurnPanicRecoveredLogged TestSubagentPanicRecoveredLogged
go_tests ./internal/provider TestStreamPanicRecoveredEvent
CMD
expect_ok "phase 2 part: -run PanicRecovered" <<'CMD'
go test -count=1 ./internal/agent ./internal/tools ./internal/provider -run 'PanicRecovered'
CMD
expect_ok "crashlog.Recovered is called at the four recover sites" <<'CMD'
for f in internal/tools/registry.go internal/agent/loop.go internal/agent/subagent.go internal/provider/client.go; do grep -q -e 'crashlog.Recovered' $f || { echo "missing in $f"; exit 1; }; done
CMD
expect_ok "user-visible texts are unchanged" <<'CMD'
grep -q -e 'failed: panic: ' internal/tools/registry.go && grep -q -e 'internal error: ' internal/agent/loop.go && grep -q -e 'subagent panic: ' internal/agent/subagent.go
CMD
expect_fail "negative: crash records must not reference history or tool output in the wiring" <<'CMD'
no_match -n -e 'crashlog.Recovered([^)]*\(Output\|Arguments\|history\|hist\)' internal/tools/registry.go internal/agent/loop.go internal/agent/subagent.go internal/provider/client.go
CMD
expect_ok "race detector on agent, tools, provider" <<'CMD'
go test -race -count=1 ./internal/agent ./internal/tools ./internal/provider
CMD
expect_ok "security files, go.mod and go.sum unchanged since 1ccbbc7" <<'CMD'
v10_guard
CMD
expect_ok "make check" <<'CMD'
make check
CMD
finish
