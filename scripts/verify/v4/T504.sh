#!/usr/bin/env bash
# Verification for T504 (supervisor-owned; workers never edit it). Exit 0 = pass.
. "$(dirname "$0")/../_lib.sh"
expect_ok "named tab click tests pass" <<'CMD'
go_tests ./internal/tui TestTabButtons TestTabClickSwitch
CMD
expect_ok "tab bar, close/save and mouse tests still green" <<'CMD'
go_tests ./internal/tui TestTabBarBounds TestTabBarZones TestCtrlTOpensEightTabsThenLimit TestCtrlWSavesAndSwitchesLeftNeighbour TestCtrlWLastTabExitsLikeExit TestCloseTabSavesSession TestInputClick TestCopySelection TestCopyClickNoDrag && go test -count=1 -run 'Tab|Copy|Input' ./internal/tui
CMD
expect_ok "Ctrl+T and the + click share openTab" <<'CMD'
grep -q -e 'func (m Model) openTab() (Model, tea.Cmd)' internal/tui/*.go && test "$(grep -h --include='*.go' --exclude='*_test.go' -r -e 'openTab()' internal/tui | grep -v -e 'func (m Model) openTab' | wc -l)" -ge 2
CMD
expect_fail "negative: the click path does not re-implement saving" <<'CMD'
no_match -n -e 'store.Save' -e 'session.NewStore' -e 'os.WriteFile' internal/tui/mouse.go $(ls internal/tui/tabclick.go 2>/dev/null)
CMD
expect_ok "go.mod and go.sum unchanged" <<'CMD'
git diff --quiet 4fabda9 -- go.mod go.sum
CMD
expect_fail "negative: no protected path or other package touched" <<'CMD'
out="$(git status --porcelain --untracked-files=all -- README.md Plan.md prompts .claude scripts/verify versions/v4/spec.md versions/v4/plan.md versions/v4/tasks internal/provider internal/tools internal/agent internal/config internal/session cmd internal/tui/save.go)" || exit 125
printf '%s' "$out" | no_match -e .
CMD
expect_ok "make check" <<'CMD'
make check
CMD
finish
