#!/usr/bin/env bash
# Verification for T101 (supervisor-owned; workers never edit it). Exit 0 = pass.
. "$(dirname "$0")/../_lib.sh"
expect_ok "reaper files and exported KillAll exist" <<'CMD'
test -f internal/tools/reaper_linux.go && test -f internal/tools/reaper_other.go && has_symbols ./internal/tools KillAll
CMD
expect_ok "worker tests: TestReaper Background/TermIgnored/Idempotent/ExitedGroup/Bystander/Timing" <<'CMD'
go_tests ./internal/tools TestReaperBackground TestReaperTermIgnored TestReaperIdempotent TestReaperExitedGroup TestReaperBystander TestReaperTiming
CMD
expect_ok "reaper tests pass under -race" <<'CMD'
go test -race -count=1 -run Reaper ./internal/tools
CMD
expect_ok "supervisor tests: background killed, TERM-ignoring killed, bystanders survive, 2nd call fast" <<'CMD'
trap 'rm -f internal/tools/zz_v12_helpers_test.go internal/tools/zz_v12_t101_test.go' EXIT
cp .harness/verify/v12/tools_helpers_test.go.txt internal/tools/zz_v12_helpers_test.go
cp .harness/verify/v12/tools_t101_test.go.txt internal/tools/zz_v12_t101_test.go
go_tests ./internal/tools TestV12VerifyBackgroundKilled TestV12VerifyTermIgnoredKilled TestV12VerifySecondCallFast TestV12VerifyBystanderSurvives
CMD
expect_ok "non-linux build compiles (reaper_other.go)" <<'CMD'
GOOS=darwin GOARCH=arm64 go build ./internal/tools
CMD
expect_ok "existing bash cancel/timeout tests still pass" <<'CMD'
go test -count=1 ./internal/tools
CMD
expect_fail "negative: no syscall.Kill outside internal/tools (non-test)" <<'CMD'
grep -rn 'syscall.Kill' --include='*.go' . | grep -v '_test.go' | grep -v 'internal/tools/' | grep .
CMD
expect_fail "negative: no os/exec importer besides bash.go (non-test)" <<'CMD'
grep -rln '"os/exec"' --include='*.go' . | grep -v '_test.go' | grep -v '^./internal/tools/bash.go$' | grep .
CMD
expect_fail "negative: no leftover test sleeps after the run" <<'CMD'
pgrep -f '^sleep 319[0-9]$'
CMD
expect_ok "make check" <<'CMD'
make check
CMD
finish
