#!/usr/bin/env bash
# Verification for T503 (supervisor-owned; workers never edit it). Exit 0 = pass.
. "$(dirname "$0")/../_lib.sh"
expect_ok "R71: TestApprovalWebFetchURL passes" <<'CMD'
go_tests ./internal/tui TestApprovalWebFetchURL
CMD
expect_ok "negative R69: TestApprovalWebFetchSecret passes (no SECRET123 in the view)" <<'CMD'
go_tests ./internal/tui TestApprovalWebFetchSecret || exit 1
grep -q -e 'SECRET123' internal/tui/approval_webfetch_test.go && grep -q -F -e '[redacted]' internal/tui/approval_webfetch_test.go
CMD
expect_ok "web_search approval tests still pass" <<'CMD'
go_tests ./internal/tui TestApprovalWebSearchQuery TestApprovalWebSearchSecret
CMD
expect_ok "argsSummary maps web_fetch to url" <<'CMD'
grep -A1 -e 'case "web_fetch"' internal/tui/approval.go | grep -q -e 'key = "url"'
CMD
expect_fail "negative: the tests never contact a non-loopback host" <<'CMD'
no_match -n -E -e 'https?://[a-z]' internal/tui/approval_webfetch_test.go
CMD
expect_ok "tui package passes" <<'CMD'
go test ./internal/tui -count=1
CMD
expect_ok "scope: only internal/tui/approval.go and the new test changed" <<'CMD'
changed="$( { git diff --name-only HEAD; git ls-files -o --exclude-standard; } | sort -u | grep -v -e '^versions/v8/Tasks.md$')"
printf '%s\n' "$changed"
test -z "$(printf '%s\n' "$changed" | grep -v -e '^$' -e '^internal/tui/approval.go$' -e '^internal/tui/approval_webfetch_test.go$')"
CMD
expect_ok "make check" <<'CMD'
make check
CMD
finish
