#!/usr/bin/env bash
# Verification for T601 (supervisor-owned; workers never edit it). Exit 0 = pass.
. "$(dirname "$0")/../_lib.sh"
expect_ok "named tui tests pass" <<'CMD'
go_tests ./internal/tui TestNewline TestAltEnterUnchanged TestHelp
CMD
expect_ok "named cmd/yip tests pass" <<'CMD'
go_tests ./cmd/yip TestHelpFlagNoShiftEnter TestNoKeyReportingRequest TestHelpFlag
CMD
expect_ok "all help and newline tests still green" <<'CMD'
go test -count=1 -run 'Help|Newline|AltEnter' ./internal/tui ./cmd/yip
CMD
expect_fail "negative: no modifyOtherKeys anywhere in cmd or internal Go code" <<'CMD'
no_match -rn -i --include='*.go' -e 'modifyOtherKeys' cmd internal
CMD
expect_fail "negative: no key reporting escape or CSI mapping left" <<'CMD'
no_match -rn --include='*.go' --exclude='*_test.go' -F -e '[>4' -e 'keyFromCSI' -e 'enableKeyReporting' -e 'keyReportingLevel' cmd internal
CMD
expect_fail "negative: shift+enter is not bound or documented" <<'CMD'
no_match -rn -i --include='*.go' --exclude='*_test.go' -e 'shift+enter' internal/tui cmd
CMD
expect_fail "negative: keyseq.go and its test are deleted" <<'CMD'
test -e internal/tui/keyseq.go || test -e internal/tui/keyseq_test.go
CMD
expect_ok "mouse capture kept" <<'CMD'
grep -q -e 'WithMouseCellMotion' cmd/yip/main.go
CMD
expect_ok "go.mod and go.sum unchanged" <<'CMD'
git diff --quiet 4fabda9 -- go.mod go.sum
CMD
expect_fail "negative: no protected path or other package touched" <<'CMD'
out="$(git status --porcelain --untracked-files=all -- README.md Plan.md prompts .claude scripts/verify versions/v4/spec.md versions/v4/plan.md versions/v4/tasks internal/provider internal/tools internal/agent internal/config internal/session)" || exit 125
printf '%s' "$out" | no_match -e .
CMD
expect_ok "make check" <<'CMD'
make check
CMD
finish
