#!/usr/bin/env bash
# Verification for T402 (supervisor-owned; workers never edit it). Exit 0 = pass.
. "$(dirname "$0")/../_lib.sh"
expect_ok "TestHelpExitCleanup passes" <<'CMD'
go_tests ./internal/tui TestHelpExitCleanup
CMD
expect_ok "all Help tests pass" <<'CMD'
go test -count=1 -run Help ./internal/tui ./cmd/yip
CMD
expect_ok "yip --help prints the cleanup line" <<'CMD'
go run ./cmd/yip --help | grep -qF 'processes started by commands (also background jobs and servers) are terminated after a 2 s grace'
CMD
expect_fail "negative: the line is not printed twice" <<'CMD'
test "$(go run ./cmd/yip --help | grep -cF 'are terminated after a 2 s grace')" -ne 1
CMD
expect_ok "make check" <<'CMD'
make check
CMD
finish
