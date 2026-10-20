#!/usr/bin/env bash
# Verification for T505 (supervisor-owned; workers never edit it). Exit 0 = pass.
. "$(dirname "$0")/../_lib.sh"
expect_ok "tests: approval prompt" <<'CMD'
go_tests ./internal/tui TestApprovalPromptShown TestApprovalDeny TestApprovalYes TestApprovalAlways TestApprovalAutoMode TestApprovalIgnoresOtherKeys
CMD
expect_ok "make check" <<'CMD'
make check
CMD
expect_ok "probe builds against the prescribed API" <<'CMD'
build_probe scripts/verify/v1/probes/T505.go
CMD
expect_ok "probe: prompt shown, y runs the command" <<'CMD'
probe_ok yes
CMD
expect_fail "negative: manual-mode bash never runs unprompted or after n" <<'CMD'
probe_rejects deny
CMD
finish
