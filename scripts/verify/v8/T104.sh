#!/usr/bin/env bash
# Verification for T104 (supervisor-owned; workers never edit it). Exit 0 = pass.
. "$(dirname "$0")/../_lib.sh"
expect_ok "TestConfigLeftoverKey and TestSearchNotInBaseURLs pass" <<'CMD'
go_tests ./internal/config TestConfigLeftoverKey TestSearchNotInBaseURLs
CMD
expect_ok "leftover test builds the key as \\x6aev and covers the unset env var" <<'CMD'
f=internal/config/leftover_test.go
grep -q -F -e 'x6aev' "$f" && grep -q -e 'LEFTOVER_UNSET' "$f" && grep -q -e 'LEFTOVER_SET' "$f" && grep -q -e 'leftover.example' "$f"
CMD
expect_ok "jev files of internal/config are deleted" <<'CMD'
test ! -e internal/config/jev.go && test ! -e internal/config/jev_test.go
CMD
expect_fail "negative: Config.AllowedBaseURLs and Config.Jev are gone" <<'CMD'
for s in Config.AllowedBaseURLs Config.Jev DefaultJevBaseURL; do
	if go doc ./internal/config "$s" >/dev/null 2>&1; then echo "still exported: $s"; exit 0; fi
done
go doc ./internal/config Config.BaseURLs >/dev/null 2>&1 || exit 125
exit 1
CMD
expect_fail "negative (Phase 1 exit grep): no jev, systemone or typesafe in *.go, *.json, Makefile" <<'CMD'
hits="$(grep -rniE 'jev|systemone|typesafe' --include='*.go' --include='*.json' --include='Makefile' . | grep -v -e '^./scripts/verify/' -e '^./versions/' -e '^./.git/')"
printf '%s\n' "$hits"
test -n "$hits"
CMD
expect_ok "env.go, redact.go, pathguard.go, bash.go unchanged since a99a82f" <<'CMD'
git diff --quiet a99a82f -- internal/config/env.go internal/config/redact.go internal/tools/pathguard.go internal/tools/bash.go
CMD
expect_ok "config package tests pass" <<'CMD'
go test ./internal/config -count=1
CMD
expect_ok "scope: only internal/config changed (tracked and untracked)" <<'CMD'
changed="$( { git diff --name-only HEAD; git ls-files -o --exclude-standard; } | sort -u | grep -v -e '^versions/v8/Tasks.md$')"
printf '%s\n' "$changed"
test -z "$(printf '%s\n' "$changed" | grep -v -e '^$' -e '^internal/config/')"
CMD
expect_ok "make check" <<'CMD'
make check
CMD
finish
