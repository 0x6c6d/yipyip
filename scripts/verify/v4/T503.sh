#!/usr/bin/env bash
# Verification for T503 (supervisor-owned; workers never edit it). Exit 0 = pass.
. "$(dirname "$0")/../_lib.sh"
expect_ok "named tab bar tests pass" <<'CMD'
go_tests ./internal/tui TestTabBarBounds TestTabBarZones TestTabBarRendersSegments TestTabBarNotShownWithOneFreshTab
CMD
expect_ok "all tab tests still green" <<'CMD'
go test -count=1 -run 'Tab|CtrlT|CtrlW|Alt' ./internal/tui
CMD
expect_ok "adapted test expects the new format" <<'CMD'
grep -q -F -e '1:first question × 2:new session × +' internal/tui/tabbar_test.go && grep -q -F -e '1:new session × +' internal/tui/tabbar_test.go
CMD
expect_ok "one layout function with hit zones" <<'CMD'
grep -q -e 'func (m Model) tabBarLayout' -e 'func (m \*Model) tabBarLayout' internal/tui/tabbar.go && grep -q -e 'tabZoneClose' internal/tui/tabbar.go && grep -q -e 'tabZonePlus' internal/tui/tabbar.go
CMD
expect_fail "negative: the bar is not rendered anywhere else" <<'CMD'
no_match -rn --include='*.go' --exclude='*_test.go' -e 'tabCurrentStyle' internal/tui | grep -v -e '^internal/tui/tabbar.go:' | no_match -e .
CMD
expect_ok "go.mod and go.sum unchanged" <<'CMD'
git diff --quiet 4fabda9 -- go.mod go.sum
CMD
expect_fail "negative: no protected path or other package touched" <<'CMD'
out="$(git status --porcelain --untracked-files=all -- README.md Plan.md prompts .claude scripts/verify versions/v4/spec.md versions/v4/plan.md versions/v4/tasks internal/provider internal/tools internal/agent internal/config internal/session cmd)" || exit 125
printf '%s' "$out" | no_match -e .
CMD
expect_ok "make check" <<'CMD'
make check
CMD
finish
