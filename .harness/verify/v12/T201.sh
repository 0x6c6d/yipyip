#!/usr/bin/env bash
# Verification for T201 (supervisor-owned; workers never edit it). Exit 0 = pass.
. "$(dirname "$0")/../_lib.sh"
expect_ok "exported InitSubreaper and KillAll" <<'CMD'
has_symbols ./internal/tools InitSubreaper KillAll
CMD
expect_ok "worker tests: Subreaper/Setsid/DoubleFork/Zombie + T101 reaper tests" <<'CMD'
go_tests ./internal/tools TestReaperSubreaper TestReaperSetsid TestReaperDoubleFork TestReaperZombie TestReaperBackground TestReaperTermIgnored TestReaperIdempotent TestReaperExitedGroup TestReaperBystander TestReaperTiming
CMD
expect_ok "reaper tests pass under -race" <<'CMD'
go test -race -count=1 -run Reaper ./internal/tools
CMD
expect_ok "supervisor tests: subreaper set, setsid/double-fork killed, no zombie stall, same-session bystander survives" <<'CMD'
trap 'rm -f internal/tools/zz_v12_helpers_test.go internal/tools/zz_v12_t101_test.go internal/tools/zz_v12_t201_test.go' EXIT
cp .harness/verify/v12/tools_helpers_test.go.txt internal/tools/zz_v12_helpers_test.go
cp .harness/verify/v12/tools_t101_test.go.txt internal/tools/zz_v12_t101_test.go
cp .harness/verify/v12/tools_t201_test.go.txt internal/tools/zz_v12_t201_test.go
go_tests ./internal/tools TestV12VerifySubreaperSet TestV12VerifySetsidKilled TestV12VerifyDoubleForkKilled TestV12VerifyZombieNoStall TestV12VerifySameSessionBystander TestV12VerifyBackgroundKilled TestV12VerifyTermIgnoredKilled TestV12VerifySecondCallFast TestV12VerifyBystanderSurvives
CMD
expect_ok "non-linux build compiles" <<'CMD'
GOOS=darwin GOARCH=arm64 go build ./internal/tools
CMD
expect_fail "negative: no Wait4(-1) in the reaper" <<'CMD'
grep -rn -e 'Wait4(-1' -e 'Wait4( -1' internal/tools --include='*.go' | grep -v '_test.go' | grep .
CMD
expect_fail "negative: no syscall.Kill outside internal/tools (non-test)" <<'CMD'
grep -rn 'syscall.Kill' --include='*.go' . | grep -v '_test.go' | grep -v 'internal/tools/' | grep .
CMD
expect_fail "negative: no leftover test sleeps after the run" <<'CMD'
pgrep -f '^sleep 31[89][0-9]$'
CMD
expect_ok "make check" <<'CMD'
make check
CMD
finish
