#!/usr/bin/env bash
# Verification for T604 (supervisor-owned; workers never edit it). Exit 0 = pass.
. "$(dirname "$0")/../_lib.sh"
expect_ok "e2e tests: /compact" <<'CMD'
go_tests ./e2e TestCompactShrinksHistory TestAutoCompact80 TestAutoCompactOff
CMD
expect_ok "DoD D8: go test ./e2e -run Compact" <<'CMD'
go test -count=1 ./e2e -run Compact
CMD
expect_ok "e2e undo tests still pass" <<'CMD'
go test -count=1 ./e2e -run Undo
CMD
expect_ok "make check" <<'CMD'
make check
CMD
finish
