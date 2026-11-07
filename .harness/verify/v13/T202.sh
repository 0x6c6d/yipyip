#!/usr/bin/env bash
# Verification for T202 (supervisor-owned; workers never edit it). Exit 0 = pass.
. "$(dirname "$0")/../_lib.sh"
expect_ok "named e2e tests pass" <<'CMD'
go_tests ./e2e TestGlobalAgentsNew TestGlobalAgentsClear TestGlobalAgentsInit
CMD
expect_ok "tests run /new, /clear and /init and snapshot the config dir" <<'CMD'
f=e2e/globalagents_test.go
grep -q -F '"/new"' "$f" && grep -q -F '"/clear"' "$f" && grep -q -F '"/init"' "$f" && grep -q -F 'GLOBAL-INIT-E2E' "$f" && grep -q -F '.config' "$f"
CMD
expect_fail "negative: e2e tests do not set GlobalDir directly (must go through HOME)" <<'CMD'
no_match -n -e 'GlobalDir' e2e/globalagents_test.go e2e/helpers_test.go
CMD
expect_fail "negative: init.go does not read the global file" <<'CMD'
no_match -n -e 'GlobalDir' -e 'readGlobalAgentsFile' internal/agent/init.go
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
