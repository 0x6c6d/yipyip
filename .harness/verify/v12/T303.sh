#!/usr/bin/env bash
# Verification for T303 (supervisor-owned; workers never edit it). Exit 0 = pass.
. "$(dirname "$0")/../_lib.sh"
expect_ok "SignalQuitMsg exported, launchUI disables Bubble Tea's signal handler and handles SIGTERM/SIGHUP" <<'CMD'
has_symbols ./internal/tui SignalQuitMsg && grep -q 'tea.WithoutSignalHandler()' cmd/yip/main.go && grep -q 'syscall.SIGHUP' cmd/yip/main.go && grep -q 'tui.SignalQuitMsg{}' cmd/yip/main.go
CMD
expect_ok "quit() shuts down tab sessions" <<'CMD'
grep -q 'Shutdown()' internal/tui/model.go
CMD
expect_ok "tui tests: quit shuts down sessions, signal quit saves" <<'CMD'
go_tests ./internal/tui TestQuitShutsDownSessions TestSignalQuitMsgSaves
CMD
expect_ok "e2e tests: /quit, Ctrl+C, SIGTERM, SIGHUP leave no process" <<'CMD'
go_tests ./e2e TestCleanupTUIQuit TestCleanupTUICtrlC TestCleanupTUISigterm TestCleanupTUISighup
CMD
expect_ok "existing exit-save and crash tests still pass" <<'CMD'
go test -count=1 -run 'Save|Quit|Crash|Guard' ./internal/tui ./cmd/yip
CMD
expect_fail "negative: internal/tui never signals or reaps itself" <<'CMD'
grep -rn -e 'syscall.Kill' -e 'KillAll' internal/tui --include='*.go' | grep -v '_test.go' | grep .
CMD
expect_fail "negative: launchUI does not call KillAll (exitCleanup in run does)" <<'CMD'
grep -n 'KillAll' cmd/yip/main.go | grep -v 'exitCleanup = func' | grep .
CMD
expect_fail "negative: no leftover test sleeps after the run" <<'CMD'
pgrep -f '^sleep 305[0-9]$'
CMD
expect_ok "make check" <<'CMD'
make check
CMD
finish
