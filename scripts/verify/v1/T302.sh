#!/usr/bin/env bash
# Verification for T302 (supervisor-owned; workers never edit it). Exit 0 = pass.
. "$(dirname "$0")/../_lib.sh"
expect_ok "tests: bash runner" <<'CMD'
go_tests ./internal/tools TestBashExitCode TestBashCombinedOutput TestBashDir TestBashTimeout TestBashTimeoutKillsGroup TestBashCancel TestBashTruncation TestClampTimeout TestSecretBashEnv
CMD
expect_ok "make check" <<'CMD'
make check
CMD
expect_ok "probe builds against the prescribed API" <<'CMD'
build_probe scripts/verify/v1/probes/T302.go
CMD
expect_ok "probe: exit code, combined output, working dir" <<'CMD'
probe_ok exit
CMD
expect_ok "probe: truncation marker (runes)" <<'CMD'
probe_ok trunc
CMD
expect_ok "probe: timeout clamping and constants" <<'CMD'
probe_ok clamp
CMD
expect_fail "negative: timeout stops sleep 30 quickly" <<'CMD'
probe_rejects timeout
CMD
expect_fail "negative: background grandchild killed with the process group" <<'CMD'
probe_rejects group
CMD
expect_fail "negative: context cancel stops the command" <<'CMD'
probe_rejects cancel
CMD
expect_fail "negative: stripped variable does not reach the command" <<'CMD'
probe_rejects env
CMD
expect_fail "negative: os/exec used by no other non-test file" <<'CMD'
out="$(grep -rlF '"os/exec"' --include='*.go' --exclude='*_test.go' cmd internal)"
printf '%s\n' "$out" | grep -v -x 'internal/tools/bash.go' | no_match .
CMD
finish
