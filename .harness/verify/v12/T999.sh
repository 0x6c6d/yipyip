#!/usr/bin/env bash
# Verification for T999 (supervisor-owned; workers never edit it). Exit 0 = pass.
. "$(dirname "$0")/../_lib.sh"
expect_ok "reaper tests (R101-R103, R105)" <<'CMD'
go_tests ./internal/tools TestReaperBackground TestReaperTermIgnored TestReaperIdempotent TestReaperExitedGroup TestReaperBystander TestReaperTiming TestReaperSetsid TestReaperSubreaper TestReaperDoubleFork TestReaperZombie
CMD
expect_ok "reaper tests under -race" <<'CMD'
go test -race -count=1 -run Reaper ./internal/tools
CMD
expect_ok "supervisor reaper tests (incl. bystanders survive)" <<'CMD'
trap 'rm -f internal/tools/zz_v12_helpers_test.go internal/tools/zz_v12_t101_test.go internal/tools/zz_v12_t201_test.go' EXIT
cp .harness/verify/v12/tools_helpers_test.go.txt internal/tools/zz_v12_helpers_test.go
cp .harness/verify/v12/tools_t101_test.go.txt internal/tools/zz_v12_t101_test.go
cp .harness/verify/v12/tools_t201_test.go.txt internal/tools/zz_v12_t201_test.go
go_tests ./internal/tools TestV12VerifyBackgroundKilled TestV12VerifyTermIgnoredKilled TestV12VerifySecondCallFast TestV12VerifyBystanderSurvives TestV12VerifySubreaperSet TestV12VerifySetsidKilled TestV12VerifyDoubleForkKilled TestV12VerifyZombieNoStall TestV12VerifySameSessionBystander
CMD
expect_ok "Shutdown tests (R101, R106)" <<'CMD'
go_tests ./internal/agent TestShutdownReapsMainAndSubagent TestShutdownForegroundCommand TestShutdownIdempotent
CMD
expect_ok "cleanup e2e tests (R104a-d, R105)" <<'CMD'
go_tests ./e2e TestCleanupTUIQuit TestCleanupTUICtrlC TestCleanupTUISigterm TestCleanupTUISighup TestCleanupHeadless TestCleanupHeadlessTimeout TestCleanupHeadlessMaxCalls TestCleanupHeadlessSignal TestCleanupCrash TestCleanupOutsider
CMD
expect_ok "supervisor e2e tests (headless and crash reap, outsider survives)" <<'CMD'
trap 'rm -f e2e/zz_v12_t302_test.go' EXIT
cp .harness/verify/v12/e2e_t302_test.go.txt e2e/zz_v12_t302_test.go
go_tests ./e2e TestV12VerifyHeadlessReaps TestV12VerifyCrashReaps TestV12VerifyOutsiderSurvives
CMD
expect_ok "TUI quit/signal unit tests" <<'CMD'
go_tests ./internal/tui TestQuitShutsDownSessions TestSignalQuitMsgSaves
CMD
expect_ok "prompt sentence (R107) and help line (R108)" <<'CMD'
go_tests ./internal/agent TestPromptExitCleanup && go test -count=1 -run Prompt ./internal/agent && go_tests ./internal/tui TestHelpExitCleanup && go test -count=1 -run Help ./internal/tui
CMD
expect_ok "README verification script (R108)" <<'CMD'
bash .harness/verify/v12/T403.sh
CMD
expect_ok "CLAUDE.md verification script" <<'CMD'
bash .harness/verify/v12/T404.sh
CMD
expect_fail "negative: syscall.Kill outside internal/tools (non-test)" <<'CMD'
grep -rn 'syscall.Kill' --include='*.go' . | grep -v '_test.go' | grep -v 'internal/tools/' | grep .
CMD
expect_fail "negative: os/exec imported outside internal/tools/bash.go (non-test)" <<'CMD'
grep -rln '"os/exec"' --include='*.go' . | grep -v '_test.go' | grep -v '^./internal/tools/bash.go$' | grep .
CMD
expect_fail "negative: supervisor test copies left in the tree" <<'CMD'
ls internal/tools/zz_v12_* e2e/zz_v12_* 2>/dev/null | grep .
CMD
expect_fail "negative: leftover test sleeps" <<'CMD'
pgrep -f '^sleep 3[0-3][0-9][0-9]$'
CMD
expect_ok "make check" <<'CMD'
make check
CMD
finish
