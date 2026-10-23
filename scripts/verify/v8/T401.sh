#!/usr/bin/env bash
# Verification for T401 (supervisor-owned; workers never edit it). Exit 0 = pass.
. "$(dirname "$0")/../_lib.sh"
expect_ok "R71: TestGatedToolsV8 passes" <<'CMD'
go_tests ./internal/agent TestGatedToolsV8
CMD
expect_ok "existing approval tests pass" <<'CMD'
go test ./internal/agent -count=1 -run 'Approval|Gate|Gated' >/dev/null || { go test ./internal/agent -count=1 -run 'Approval|Gate|Gated' 2>&1 | tail -n 30; exit 1; }
CMD
expect_ok "approval.go: exactly one added line \"web_fetch\":  true, and nothing removed" <<'CMD'
d="$(git diff -U0 a99a82f -- internal/agent/approval.go | grep -E '^[-+][^-+]')"
printf '%s\n' "$d"
test "$(printf '%s\n' "$d" | grep -c .)" -eq 1 &&
printf '%s\n' "$d" | grep -q -x -F -e '+	"web_fetch":  true,'
CMD
expect_ok "the new line directly follows the web_search line" <<'CMD'
grep -A1 -F -e '"web_search": true,' internal/agent/approval.go | tail -n 1 | grep -q -x -F -e '	"web_fetch":  true,'
CMD
expect_fail "negative: with the web_fetch line removed (copy), TestGatedToolsV8 fails" <<'CMD'
m="$VTMP/mut"; mkdir -p "$m"
git ls-files -z -c -o --exclude-standard | tar --null --ignore-failed-read -T - -cf - 2>/dev/null | tar -xf - -C "$m" || exit 125
cd "$m" || exit 125
grep -q -F -e '"web_fetch":  true,' internal/agent/approval.go || exit 125
sed -i '/"web_fetch":  true,/d' internal/agent/approval.go
out="$(go test ./internal/agent -count=1 -run '^TestGatedToolsV8$' 2>&1)"
printf '%s\n' "$out" | tail -n 20
printf '%s\n' "$out" | grep -q -e '^--- FAIL: TestGatedToolsV8' && exit 1
exit 0
CMD
expect_ok "scope: only approval.go and the new test changed under internal/" <<'CMD'
changed="$( { git diff --name-only HEAD; git ls-files -o --exclude-standard; } | sort -u | grep -v -e '^versions/v8/Tasks.md$')"
printf '%s\n' "$changed"
test -z "$(printf '%s\n' "$changed" | grep -v -e '^$' -e '^internal/agent/approval.go$' -e '^internal/agent/gated_v8_test.go$')"
CMD
expect_ok "make check" <<'CMD'
make check
CMD
finish
