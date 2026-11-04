#!/usr/bin/env bash
# Verification for T402 (supervisor-owned; workers never edit it). Exit 0 = pass.
. "$(dirname "$0")/../_lib.sh"
expect_ok "tab bar and key tests incl. negatives" <<'CMD'
go_tests ./internal/tui TestTabBarRendersSegments TestCtrlTOpensEightTabsThenLimit TestCtrlWSavesAndSwitchesLeftNeighbour TestCtrlWLastTabExitsLikeExit TestTabCycleKeys TestAltDigitJumps TestTabKeyStillCompletes
CMD
expect_ok "whole tui package (mode + input regression included)" <<'CMD'
go test -count=1 ./internal/tui
CMD
expect_ok "key strings bound (Ctrl+T/W, cycles, Alt digits)" <<'CMD'
f=internal/tui/model.go
grep -q 'ctrl+t' $f && grep -q 'ctrl+w' $f && grep -q 'ctrl+tab' $f && grep -q 'alt+1' $f
CMD
expect_ok "tab bar renders (tabbar.go exists)" <<'CMD'
test -f internal/tui/tabbar.go
CMD
expect_fail "negative: ctrl+shift+tab is bound exactly once (cycle), leaving plain shift+tab for the mode toggle" <<'CMD'
no_match -rn --include='*.go' -e 'ctrl+shift+tab' internal/tui/model.go internal/tui/tabbar.go
CMD
expect_fail "negative: protected paths untouched (transport + tools at 9519824)" <<'CMD'
out="$(git diff --stat 9519824 -- internal/provider/transport.go internal/tools && git status --porcelain --untracked-files=all -- internal/provider/transport.go internal/tools)" || exit 125
printf '%s' "$out" | no_match -e .
CMD
expect_ok "make check" <<'CMD'
make check
CMD
finish
