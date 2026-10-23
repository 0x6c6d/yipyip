#!/usr/bin/env bash
# Verification for T101 (supervisor-owned; workers never edit it). Exit 0 = pass.
. "$(dirname "$0")/../_lib.sh"
expect_ok "TestUnknownCommandRemoved and help tests pass" <<'CMD'
go_tests ./internal/tui TestUnknownCommandRemoved || exit 1
go test ./internal/tui -count=1 -run 'Help' >/dev/null || { go test ./internal/tui -count=1 -run 'Help' 2>&1 | tail -n 30; exit 1; }
CMD
expect_ok "the test builds the old name as \\x6aev" <<'CMD'
grep -q -F -e 'x6aev' internal/tui/removed_cmd_test.go
CMD
expect_ok "jev files of tui and e2e are deleted" <<'CMD'
for f in internal/tui/jev.go internal/tui/jev_test.go e2e/jev_test.go e2e/jev_norisk_test.go; do
	test ! -e "$f" || { echo "still present: $f"; exit 1; }
done
CMD
expect_fail "negative: no jev, typesafe or systemone in internal/tui or e2e" <<'CMD'
no_match -rniE -e 'jev|systemone|typesafe' internal/tui e2e
CMD
expect_fail "negative: no SetJev, JevOn, TurnModel or jevOn use in internal/tui" <<'CMD'
no_match -rn -e 'SetJev' -e 'JevOn' -e 'TurnModel' -e 'jevOn' internal/tui
CMD
expect_ok "scope: only internal/tui and e2e changed (tracked and untracked)" <<'CMD'
changed="$( { git diff --name-only HEAD; git ls-files -o --exclude-standard; } | sort -u | grep -v -e '^versions/v8/Tasks.md$')"
printf '%s\n' "$changed"
test -z "$(printf '%s\n' "$changed" | grep -v -e '^$' -e '^internal/tui/' -e '^e2e/')"
CMD
expect_ok "make check" <<'CMD'
make check
CMD
finish
