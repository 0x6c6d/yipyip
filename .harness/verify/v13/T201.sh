#!/usr/bin/env bash
# Verification for T201 (supervisor-owned; workers never edit it). Exit 0 = pass.
. "$(dirname "$0")/../_lib.sh"
expect_ok "named e2e tests pass" <<'CMD'
go_tests ./e2e TestGlobalAgentsTUI TestGlobalAgentsHeadless TestGlobalAgentsSubagent TestGlobalAgentsAbsentE2E
CMD
expect_ok "tests use the real HOME config dir and the binary" <<'CMD'
f=e2e/globalagents_test.go
grep -q -F 'runYip(' "$f" && grep -q -F 'spawn_agent' "$f" && grep -q -F '# Global AGENTS.md' "$f"
CMD
expect_fail "negative: e2e tests do not set GlobalDir directly (must go through HOME)" <<'CMD'
no_match -n -e 'GlobalDir' e2e/globalagents_test.go e2e/helpers_test.go
CMD
expect_fail "negative: no changes in provider, tools, cmd/yip" <<'CMD'
out="$(git diff --name-only 81ebe7e -- internal/provider internal/tools cmd/yip; git ls-files --others --exclude-standard -- internal/provider internal/tools cmd/yip)"
[ -n "$out" ] && { echo "$out"; exit 0; }
exit 1
CMD
expect_ok "whole e2e suite, vet, gofmt" <<'CMD'
go test -count=1 ./e2e/... && go vet ./e2e/... && test -z "$(gofmt -l e2e)"
CMD
finish
