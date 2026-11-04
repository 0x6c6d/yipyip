#!/usr/bin/env bash
# Verification for T102 (supervisor-owned; workers never edit it). Exit 0 = pass.
. "$(dirname "$0")/../_lib.sh"
expect_ok "export/restore tests incl. interrupted-as-finished and the un-redacted export" <<'CMD'
go_tests ./internal/agent TestExportAfterTurn TestExportTokenCountsAndModel TestInterruptedTurnExportedAsFinished TestRestoreRoundTrip
CMD
expect_ok "whole internal/agent package (no regression)" <<'CMD'
go test -count=1 ./internal/agent
CMD
expect_fail "negative: internal/agent never imports internal/session" <<'CMD'
no_match -rn --include='*.go' -e '"yipyip/internal/session"' internal/agent
CMD
expect_fail "negative: no shell/exec in agent (export path never runs a command)" <<'CMD'
no_match -rn --include='*.go' --exclude='*_test.go' -e 'exec.Command' internal/agent
CMD
expect_fail "negative: protected paths untouched (transport + tools at 9519824)" <<'CMD'
out="$(git diff --stat 9519824 -- internal/provider/transport.go internal/tools && git status --porcelain --untracked-files=all -- internal/provider/transport.go internal/tools)" || exit 125
printf '%s' "$out" | no_match -e .
CMD
expect_ok "make check" <<'CMD'
make check
CMD
finish
