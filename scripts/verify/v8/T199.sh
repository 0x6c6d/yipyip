#!/usr/bin/env bash
# Verification for T199 (supervisor-owned; workers never edit it). Exit 0 = pass.
# Phase 1 exit of versions/v8/plan.md.
: "${VERIFY_TIMEOUT:=1200}"
. "$(dirname "$0")/../_lib.sh"
expect_fail "negative AC1: no jev, systemone or typesafe in *.go, *.json, Makefile" <<'CMD'
hits="$(grep -rniE 'jev|systemone|typesafe' --include='*.go' --include='*.json' --include='Makefile' . | grep -v -e '^./scripts/verify/' -e '^./versions/' -e '^./.git/' -e '^./.yipyip/')"
printf '%s\n' "$hits"
test -n "$hits"
CMD
expect_ok "transport.go: only comment lines differ from a99a82f" <<'CMD'
d="$(git diff -U0 a99a82f -- internal/provider/transport.go | grep -E '^[-+][^-+]' | grep -vE '^[-+]\s*//')"
printf '%s\n' "$d"
test -z "$d"
CMD
expect_ok "approval.go unchanged since a99a82f (or only the T401 web_fetch line)" <<'CMD'
d="$(git diff -U0 a99a82f -- internal/agent/approval.go | grep -E '^[-+][^-+]')"
printf '%s\n' "$d"
test -z "$d" || { test "$(printf '%s\n' "$d" | wc -l)" -eq 1 && printf '%s\n' "$d" | grep -q -E '^\+[[:space:]]*"web_fetch":[[:space:]]+true,$'; }
CMD
expect_ok "TestConfigLeftoverKey passes" <<'CMD'
go_tests ./internal/config TestConfigLeftoverKey
CMD
expect_ok "TestUnknownCommandRemoved passes" <<'CMD'
go_tests ./internal/tui TestUnknownCommandRemoved
CMD
expect_fail "negative: no jev symbol exported by agent, provider, providertest or config" <<'CMD'
for ps in "./internal/agent Session.SetJev" "./internal/agent Session.TurnModel" "./internal/provider NewJevClient" "./internal/provider/providertest NewJev" "./internal/config Config.AllowedBaseURLs" "./internal/config Config.Jev"; do
	set -- $ps
	if go doc "$1" "$2" >/dev/null 2>&1; then echo "still exported: $ps"; exit 0; fi
done
exit 1
CMD
expect_ok "go build ./..." <<'CMD'
go build ./...
CMD
expect_ok "make check" <<'CMD'
make check
CMD
finish
