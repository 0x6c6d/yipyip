#!/usr/bin/env bash
# Verification for T103 (supervisor-owned; workers never edit it). Exit 0 = pass.
. "$(dirname "$0")/../_lib.sh"
expect_ok "jev files of internal/provider are deleted" <<'CMD'
for f in internal/provider/jev.go internal/provider/jev_test.go internal/provider/jev_allow_test.go internal/provider/providertest/jev.go internal/provider/providertest/jev_test.go; do
	test ! -e "$f" || { echo "still present: $f"; exit 1; }
done
CMD
expect_fail "negative: no jev, typesafe or systemone in internal/provider" <<'CMD'
no_match -rniE -e 'jev|systemone|typesafe' internal/provider
CMD
expect_fail "negative: NewJevClient and providertest.NewJev are gone" <<'CMD'
go doc ./internal/provider NewJevClient >/dev/null 2>&1 && { echo "NewJevClient still exported"; exit 0; }
go doc ./internal/provider/providertest NewJev >/dev/null 2>&1 && { echo "NewJev still exported"; exit 0; }
go doc ./internal/provider/providertest NewFunc >/dev/null 2>&1 || exit 125
exit 1
CMD
expect_fail "negative: provider tests do not use AllowedBaseURLs" <<'CMD'
no_match -rn -e 'AllowedBaseURLs' internal/provider
CMD
expect_ok "search transport tests pass (jev test removed)" <<'CMD'
go_tests ./internal/provider TestSearchTransportReachesSearchHost TestSearchTransportRefusesProviderHost TestSearchHostRefusedForProviderClient TestSearchRedirectRefused TestSearchTransportInvalidBaseURL || exit 1
! grep -q -e 'func TestSearchHostRefusedFor[A-Z]ev' internal/provider/search_transport_test.go
CMD
expect_ok "transport.go: only comment lines differ from a99a82f" <<'CMD'
d="$(git diff -U0 a99a82f -- internal/provider/transport.go | grep -E '^[-+][^-+]' | grep -vE '^[-+]\s*//')"
printf '%s\n' "$d"
test -z "$d"
CMD
expect_ok "scope: only internal/provider changed (tracked and untracked)" <<'CMD'
changed="$( { git diff --name-only HEAD; git ls-files -o --exclude-standard; } | sort -u | grep -v -e '^versions/v8/Tasks.md$')"
printf '%s\n' "$changed"
test -z "$(printf '%s\n' "$changed" | grep -v -e '^$' -e '^internal/provider/')"
CMD
expect_ok "make check" <<'CMD'
make check
CMD
finish
