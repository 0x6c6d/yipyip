#!/usr/bin/env bash
# Verification for T505 (supervisor-owned; workers never edit it). Exit 0 = pass.
. "$(dirname "$0")/../_lib.sh"
expect_ok "TestTabKeys and the remaining tab/help tests pass" <<'CMD'
go_tests ./internal/tui TestTabKeys TestTabKeyStillCompletes TestCtrlTOpensEightTabsThenLimit TestCtrlWSavesAndSwitchesLeftNeighbour TestHelpListsNewCommands && go test -count=1 -run 'Tab|Help|CtrlT|CtrlW' ./internal/tui && go test -count=1 -run 'Help' ./cmd/yip
CMD
expect_fail "negative: the v3 switch-key tests are gone" <<'CMD'
no_match -rn --include='*_test.go' -e 'func TestTabCycleKeys' -e 'func TestAltDigitJumps' internal/tui
CMD
expect_fail "negative: help no longer lists Ctrl+Tab or Alt+N" <<'CMD'
no_match -n -e 'Ctrl+Tab' -e 'Alt+1' -e 'Ctrl+Shift+Tab' internal/tui/help.go
CMD
expect_fail "negative: key handling no longer switches tabs" <<'CMD'
no_match -n -e 'switchTab(' -e 'keyCycleBack' internal/tui/model.go
CMD
expect_ok "help lists the tab buttons" <<'CMD'
grep -q -F -e '×' internal/tui/help.go && grep -q -e '"Ctrl+T"' internal/tui/help.go && grep -q -e '"Ctrl+W"' internal/tui/help.go
CMD
expect_ok "go.mod and go.sum unchanged" <<'CMD'
git diff --quiet 4fabda9 -- go.mod go.sum
CMD
expect_fail "negative: no protected path or other package touched" <<'CMD'
out="$(git status --porcelain --untracked-files=all -- README.md Plan.md prompts .claude scripts/verify versions/v4/spec.md versions/v4/plan.md versions/v4/tasks internal/provider internal/tools internal/agent internal/config internal/session cmd/yip/main.go)" || exit 125
printf '%s' "$out" | no_match -e .
CMD
expect_ok "make check" <<'CMD'
make check
CMD
finish
