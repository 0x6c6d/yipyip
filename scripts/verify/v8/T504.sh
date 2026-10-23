#!/usr/bin/env bash
# Verification for T504 (supervisor-owned; workers never edit it). Exit 0 = pass.
. "$(dirname "$0")/../_lib.sh"
expect_ok "R68-R71: TestWebFetchE2E passes" <<'CMD'
go_tests ./e2e TestWebFetchE2E
CMD
expect_ok "negative R71: TestWebFetchE2EManualDeny passes (denied, no request)" <<'CMD'
go_tests ./e2e TestWebFetchE2EManualDeny
CMD
expect_ok "negative R69: TestWebFetchE2EDefaultClient passes (loopback refused)" <<'CMD'
go_tests ./e2e TestWebFetchE2EDefaultClient
CMD
expect_ok "AC: go test ./e2e -run WebFetch" <<'CMD'
go test ./e2e -run WebFetch -count=1
CMD
expect_ok "e2e test checks the page text, the marker line and the missing script" <<'CMD'
f=e2e/webfetch_test.go
grep -q -e 'plover' "$f" && grep -q -e 'SCRIPTX' "$f" && grep -q -F -e 'address not allowed' "$f" && grep -q -F -e 'untrusted web content' "$f"
CMD
expect_fail "negative: no non-loopback URL in the e2e test" <<'CMD'
no_match -n -E -e 'https?://[a-z]' e2e/webfetch_test.go
CMD
expect_ok "e2e package passes" <<'CMD'
go test ./e2e -count=1
CMD
expect_ok "scope: only e2e changed" <<'CMD'
changed="$( { git diff --name-only HEAD; git ls-files -o --exclude-standard; } | sort -u | grep -v -e '^versions/v8/Tasks.md$')"
printf '%s\n' "$changed"
test -z "$(printf '%s\n' "$changed" | grep -v -e '^$' -e '^e2e/')"
CMD
expect_ok "make check" <<'CMD'
make check
CMD
finish
