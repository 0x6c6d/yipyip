#!/usr/bin/env bash
# Verification for T101 (supervisor-owned; workers never edit it). Exit 0 = pass.
. "$(dirname "$0")/../_lib.sh"
expect_ok "R65/R64/R54: named agent tests pass" <<'CMD'
go_tests ./internal/agent TestMainCancelLongTurn TestMainNoIterationLimit TestSubagentIterationLimit TestCancelStopsTurn
CMD
expect_ok "R65: phase 1 exit criterion (3 named tests pass)" <<'CMD'
test "$(go test ./internal/agent -count=1 -v -run 'MainNoIterationLimit|MainCancelLongTurn|SubagentIterationLimit' | grep -cE '^--- PASS: Test(MainNoIterationLimit|MainCancelLongTurn|SubagentIterationLimit) ')" -eq 3
CMD
expect_ok "R65: TestMainCancelLongTurn passes 3x under -race" <<'CMD'
go test ./internal/agent -race -count=3 -timeout 300s -run '^TestMainCancelLongTurn$'
CMD
expect_ok "R65: test checks the 60th round, the 2 s bound and the cancelled result" <<'CMD'
f=internal/agent/loop_test.go
grep -q -e 'func TestMainCancelLongTurn(t \*testing.T)' "$f" &&
sed -n '/^func TestMainCancelLongTurn(/,/^}/p' "$f" > "$VTMP/body" &&
grep -q -e 'sleep 30' "$VTMP/body" &&
grep -q -e '2 \* time.Second' -e '2\*time.Second' "$VTMP/body" &&
grep -q -e 'CancelledMessage' -e '"cancelled"' "$VTMP/body" &&
grep -q -e 'Requests()' "$VTMP/body"
CMD
expect_ok "R64: const MaxIterations = 0" <<'CMD'
grep -n -e 'const MaxIterations = 0' internal/agent/loop.go
CMD
expect_ok "R54: const MaxSubagentIterations = 500" <<'CMD'
grep -n -e 'const MaxSubagentIterations = 500' internal/agent/loop.go
CMD
expect_fail "negative: 'iteration limit (50)' does not occur in internal/agent" <<'CMD'
no_match -n -F -e 'iteration limit (50)' internal/agent/*.go
CMD
expect_fail "negative: with MaxIterations = 59 (copy) the new test fails cleanly" <<'CMD'
m="$VTMP/mut"; mkdir -p "$m"
git ls-files -z -c -o --exclude-standard | tar --null --ignore-failed-read -T - -cf - 2>/dev/null | tar -xf - -C "$m" || exit 125
cd "$m" || exit 125
sed -i 's/^const MaxIterations = 0$/const MaxIterations = 59/' internal/agent/loop.go
grep -q -e '^const MaxIterations = 59$' internal/agent/loop.go || exit 125
out="$(go test ./internal/agent -count=1 -timeout 120s -run '^TestMainCancelLongTurn$' 2>&1)"
printf '%s\n' "$out" | tail -n 20
printf '%s\n' "$out" | grep -q -e '^--- FAIL: TestMainCancelLongTurn' && exit 1
exit 0
CMD
expect_ok "scope: protected paths have no uncommitted change" <<'CMD'
test -z "$(git status --porcelain -- Plan.md README.md versions scripts/verify prompts .claude go.mod go.sum internal/agent/approval.go ':!versions/v7/Tasks.md')"
CMD
expect_ok "scope: approval.go, go.mod, go.sum, prompts, .claude unchanged since 4f99ac0" <<'CMD'
git diff --quiet 4f99ac0 -- internal/agent/approval.go go.mod go.sum prompts .claude
CMD
expect_ok "scope: Delegation workflow section of CLAUDE.md unchanged since 4f99ac0" <<'CMD'
sec() { awk '/^## /{p = ($0 ~ /^## Delegation workflow/)} p'; }
diff <(git show 4f99ac0:CLAUDE.md | sec) <(sec < CLAUDE.md)
CMD
expect_ok "make check" <<'CMD'
make check
CMD
finish
