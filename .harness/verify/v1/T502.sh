#!/usr/bin/env bash
# Verification for T502 (supervisor-owned; workers never edit it). Exit 0 = pass.
. "$(dirname "$0")/../_lib.sh"
expect_ok "tests: status line" <<'CMD'
go_tests ./internal/tui TestStatusLineFields TestStatusTokensAfterUsage TestStatusContextSize TestStatusModeShown TestStatusNoCost TestStatusNarrow
CMD
expect_ok "DoD D6: go test ./internal/tui -run Status" <<'CMD'
go test -count=1 ./internal/tui -run Status
CMD
expect_ok "make check" <<'CMD'
make check
CMD
expect_fail "negative: no cost/price string literal in the TUI sources" <<'CMD'
no_match -rniE -e '"[^"]*cost[^"]*"' -e '"[^"]*price[^"]*"' --include='*.go' --exclude='*_test.go' internal/tui
CMD
finish
