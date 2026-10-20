#!/usr/bin/env bash
# Verification for T501 (supervisor-owned; workers never edit it). Exit 0 = pass.
. "$(dirname "$0")/../_lib.sh"
expect_ok "tests: TUI with session" <<'CMD'
go_tests ./internal/tui TestSendMessageStreams TestEscCancels TestDoubleCtrlCQuits TestNoModelMessage TestSlashNotSentToModel TestSecretTUIView
CMD
expect_ok "skeleton tests still pass" <<'CMD'
go_tests ./internal/tui TestSkeletonExit TestSkeletonEcho
CMD
expect_ok "exported API" <<'CMD'
has_symbols ./internal/tui New Options.Session EventMsg Model.Command Model.Drain Model.StatusLine
CMD
expect_ok "make check" <<'CMD'
make check
CMD
expect_fail "negative: the TUI does no networking and runs no processes" <<'CMD'
no_match -rnF -e '"os/exec"' -e '"net/http"' --include='*.go' --exclude='*_test.go' internal/tui
CMD
finish
