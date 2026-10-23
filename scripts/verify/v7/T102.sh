#!/usr/bin/env bash
# Verification for T102 (supervisor-owned; workers never edit it). Exit 0 = pass.
. "$(dirname "$0")/../_lib.sh"
expect_ok "R64: TestMainNoIterationLimit passes" <<'CMD'
go_tests ./internal/agent TestMainNoIterationLimit
CMD
expect_ok "R64: const MaxIterations = 0" <<'CMD'
grep -n -e 'const MaxIterations = 0' internal/agent/loop.go
CMD
expect_ok "R54: const MaxSubagentIterations = 500" <<'CMD'
grep -n -e 'const MaxSubagentIterations = 500' internal/agent/loop.go
CMD
expect_ok "R66: docs/usage.md says the main agent has no limit" <<'CMD'
grep -n -e 'no limit' docs/usage.md
CMD
expect_ok "R66: docs/usage.md names the subagent limit of 500" <<'CMD'
tr '\n' ' ' < docs/usage.md | grep -q -e 'stops after 500 model calls'
CMD
expect_fail "negative R66: no main-50 phrase in docs/usage.md or CLAUDE.md" <<'CMD'
no_match -n -E -e 'main agent stops after 50|50 model calls per main turn|iteration limit \(50\)' docs/usage.md CLAUDE.md
CMD
expect_fail "negative R66: the internal/agent line of CLAUDE.md names no 50" <<'CMD'
grep -e '^- `internal/agent`' CLAUDE.md > "$VTMP/line" || exit 125
no_match -n -w -e '50' "$VTMP/line"
CMD
expect_ok "R6: v1 T403.sh fails at most on the pre-existing 'HTTP error shown' probe" <<'CMD'
out="$(bash scripts/verify/v1/T403.sh 2>&1)"
printf '%s\n' "$out" | grep -q -e '^-- [0-9]* passed' || { printf '%s\n' "$out" | tail -n 20; exit 1; }
other="$(printf '%s\n' "$out" | grep -e '^FAIL' | grep -v -e 'HTTP error shown')"
test -z "$other" || { printf '%s\n' "$other"; exit 1; }
CMD
finish
