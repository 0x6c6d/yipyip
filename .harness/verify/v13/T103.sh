#!/usr/bin/env bash
# Verification for T103 (supervisor-owned; workers never edit it). Exit 0 = pass.
. "$(dirname "$0")/../_lib.sh"
expect_ok "named session tests pass" <<'CMD'
go_tests ./internal/agent TestGlobalAgentsReset TestGlobalAgentsSubagent TestGlobalAgentsInitPrompt
CMD
expect_ok "test file exists and checks the /init user prompt excludes the global note" <<'CMD'
f=internal/agent/globalagents_session_test.go
test -f "$f" && grep -q -F 'GLOBAL-INIT-NOTE' "$f" && grep -q -F 'spawn_agent' "$f"
CMD
expect_fail "negative: init.go does not read the global file" <<'CMD'
no_match -n -e 'GlobalDir' -e 'readGlobalAgentsFile' internal/agent/init.go
CMD
expect_fail "negative: no changes in provider, tools, cmd/yip" <<'CMD'
out="$(git diff --name-only 81ebe7e -- internal/provider internal/tools cmd/yip; git ls-files --others --exclude-standard -- internal/provider internal/tools cmd/yip)"
[ -n "$out" ] && { echo "$out"; exit 0; }
exit 1
CMD
expect_ok "agent package tests, vet, gofmt" <<'CMD'
go test -count=1 ./internal/agent && go vet ./internal/agent && test -z "$(gofmt -l internal/agent)"
CMD
finish
