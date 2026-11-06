#!/usr/bin/env bash
# Verification for T301 (supervisor-owned; workers never edit it). Exit 0 = pass.
. "$(dirname "$0")/../_lib.sh"
expect_ok "exported Session.Shutdown" <<'CMD'
has_symbols ./internal/agent Session.Shutdown
CMD
expect_ok "worker tests: Shutdown main+subagent, foreground, idempotent" <<'CMD'
go_tests ./internal/agent TestShutdownReapsMainAndSubagent TestShutdownForegroundCommand TestShutdownIdempotent
CMD
expect_ok "Shutdown tests under -race" <<'CMD'
go test -race -count=1 -run TestShutdown ./internal/agent
CMD
expect_fail "negative: internal/agent non-test code never signals or reaps" <<'CMD'
grep -rn -e 'syscall.Kill' -e 'KillAll' internal/agent --include='*.go' | grep -v '_test.go' | grep .
CMD
expect_fail "negative: no leftover test sleeps after the run" <<'CMD'
pgrep -f '^sleep 303[0-9]$'
CMD
expect_ok "make check" <<'CMD'
make check
CMD
finish
