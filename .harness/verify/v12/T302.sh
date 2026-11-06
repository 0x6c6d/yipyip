#!/usr/bin/env bash
# Verification for T302 (supervisor-owned; workers never edit it). Exit 0 = pass.
. "$(dirname "$0")/../_lib.sh"
expect_ok "run initialises the subreaper and reaps via exitCleanup" <<'CMD'
grep -q 'tools.InitSubreaper()' cmd/yip/main.go && grep -q 'tools.KillAll(2 \* time.Second)' cmd/yip/main.go && grep -q 'defer exitCleanup()' cmd/yip/main.go
CMD
expect_ok "headless handles SIGHUP and has the headless-end crash seam" <<'CMD'
grep -q 'syscall.SIGHUP' cmd/yip/headless.go && grep -q 'forcePanic("headless-end")' cmd/yip/headless.go
CMD
expect_ok "worker e2e tests: headless, timeout, max-calls, signal, crash, outsider" <<'CMD'
go_tests ./e2e TestCleanupHeadless TestCleanupHeadlessTimeout TestCleanupHeadlessMaxCalls TestCleanupHeadlessSignal TestCleanupCrash TestCleanupOutsider
CMD
expect_ok "supervisor e2e tests: headless and crash reap, outsider survives" <<'CMD'
trap 'rm -f e2e/zz_v12_t302_test.go' EXIT
cp .harness/verify/v12/e2e_t302_test.go.txt e2e/zz_v12_t302_test.go
go_tests ./e2e TestV12VerifyHeadlessReaps TestV12VerifyCrashReaps TestV12VerifyOutsiderSurvives
CMD
expect_ok "existing headless and crash e2e tests still pass" <<'CMD'
go test -count=1 -run 'Headless|Crash' ./e2e ./cmd/yip
CMD
expect_fail "negative: KillAll is not called from runWith unit-test paths (only via exitCleanup)" <<'CMD'
grep -rn 'KillAll' cmd/yip --include='*.go' | grep -v '_test.go' | grep -v 'exitCleanup' | grep -v 'func() { tools.KillAll' | grep .
CMD
expect_fail "negative: no syscall.Kill outside internal/tools (non-test)" <<'CMD'
grep -rn 'syscall.Kill' --include='*.go' . | grep -v '_test.go' | grep -v 'internal/tools/' | grep .
CMD
expect_fail "negative: no leftover test sleeps after the run" <<'CMD'
pgrep -f '^sleep 3(04|29)[0-9]$'
CMD
expect_ok "make check" <<'CMD'
make check
CMD
finish
