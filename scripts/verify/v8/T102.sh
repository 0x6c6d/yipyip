#!/usr/bin/env bash
# Verification for T102 (supervisor-owned; workers never edit it). Exit 0 = pass.
. "$(dirname "$0")/../_lib.sh"
expect_ok "jev files of internal/agent are deleted" <<'CMD'
for f in jev.go jev_test.go jev_noanswer_test.go jev_norisk_test.go jev_route_test.go jev_session_test.go; do
	test ! -e "internal/agent/$f" || { echo "still present: internal/agent/$f"; exit 1; }
done
CMD
expect_fail "negative: no jev, typesafe or systemone in internal/agent" <<'CMD'
no_match -rniE -e 'jev|systemone|typesafe' internal/agent
CMD
expect_ok "provider client built from cfg.BaseURLs()" <<'CMD'
grep -n -F -e 'provider.NewHTTPClient(cfg.BaseURLs())' internal/agent/session.go
CMD
expect_fail "negative: AllowedBaseURLs not used in internal/agent, internal/tui or cmd" <<'CMD'
no_match -rn -e 'AllowedBaseURLs' internal/agent internal/tui cmd
CMD
expect_fail "negative: removed Session methods are gone (SetJev JevOn JevStats JevClient TurnModel)" <<'CMD'
for s in Session.SetJev Session.JevOn Session.JevStats Session.JevClient Session.TurnModel Session.TurnTier; do
	if go doc ./internal/agent "$s" >/dev/null 2>&1; then echo "still exported: $s"; exit 0; fi
done
go doc ./internal/agent Session.Submit >/dev/null 2>&1 || exit 125
exit 1
CMD
expect_fail "negative: no routing state left in loop.go or session.go" <<'CMD'
no_match -n -e 'turnModel' -e 'turnTier' -e 'turnActive' -e 'routeTurn' -e 'accountJev' internal/agent/loop.go internal/agent/session.go
CMD
expect_ok "iteration constants unchanged" <<'CMD'
grep -q -e '^const MaxIterations = 0$' internal/agent/loop.go && grep -q -e '^const MaxSubagentIterations = 500$' internal/agent/loop.go
CMD
expect_ok "core agent tests pass" <<'CMD'
go_tests ./internal/agent TestMainNoIterationLimit TestSubagentIterationLimit TestMainCancelLongTurn
CMD
expect_ok "approval.go unchanged since a99a82f" <<'CMD'
git diff --quiet a99a82f -- internal/agent/approval.go
CMD
expect_ok "scope: only internal/agent changed (tracked and untracked)" <<'CMD'
changed="$( { git diff --name-only HEAD; git ls-files -o --exclude-standard; } | sort -u | grep -v -e '^versions/v8/Tasks.md$')"
printf '%s\n' "$changed"
test -z "$(printf '%s\n' "$changed" | grep -v -e '^$' -e '^internal/agent/')"
CMD
expect_ok "make check" <<'CMD'
make check
CMD
finish
