#!/usr/bin/env bash
# Verification for T999 (supervisor-owned; workers never edit it). Exit 0 = pass.
. "$(dirname "$0")/../_lib.sh"
expect_ok "at least 6 TestGlobalAgents unit tests pass in internal/agent" <<'CMD'
out="$(go test ./internal/agent -run GlobalAgents -count=1 -v 2>&1)" || { printf '%s\n' "$out" | tail -n 40; exit 1; }
n=$(printf '%s\n' "$out" | grep -c -e '^--- PASS: TestGlobalAgents')
echo "top-level TestGlobalAgents* passed: $n"
[ "$n" -ge 6 ]
CMD
expect_ok "GlobalAgents e2e tests pass (TUI, headless, subagent, /new, /init)" <<'CMD'
go_tests ./e2e TestGlobalAgentsTUI TestGlobalAgentsHeadless TestGlobalAgentsSubagent TestGlobalAgentsNew TestGlobalAgentsInit
CMD
expect_ok "all GlobalAgents e2e tests pass" <<'CMD'
go test ./e2e -run GlobalAgents -count=1 -v
CMD
expect_ok "README and help greps" <<'CMD'
grep -q '~/.config/yipyip/AGENTS.md' README.md && grep -q '30,000' README.md && grep -q 'AGENTS.md' internal/tui/help.go && grep -qi 'global' internal/tui/help.go
CMD
expect_ok "README task verification passes" <<'CMD'
bash .harness/verify/v13/T302.sh
CMD
expect_fail "negative: no changes in provider, tools, cmd/yip since the v13 baseline" <<'CMD'
out="$(git diff --name-only 81ebe7e -- internal/provider internal/tools cmd/yip; git ls-files --others --exclude-standard -- internal/provider internal/tools cmd/yip)"
[ -n "$out" ] && { echo "$out"; exit 0; }
exit 1
CMD
expect_fail "negative: no leftover supervisor probe file" <<'CMD'
test -e internal/agent/zz_v13probe_test.go && { echo "leftover probe"; exit 0; }
exit 1
CMD
expect_ok "make check" <<'CMD'
make check
CMD
finish
