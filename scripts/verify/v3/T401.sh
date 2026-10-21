#!/usr/bin/env bash
# Verification for T401 (supervisor-owned; workers never edit it). Exit 0 = pass.
. "$(dirname "$0")/../_lib.sh"
expect_ok "tab state tests incl. negatives" <<'CMD'
go_tests ./internal/tui TestAddTabSwitchesToNewTab TestSwitchTabSwapsState TestGlobalModelSharedAcrossTabs TestCloseTabSwitchesLeftNeighbour TestCloseLastTabQuitsAndSaves TestCloseTabSavesSession TestAddTabAtLimitNoChange TestNewOpensTabKeepsOldFile TestBackgroundTurnAppendsToItsTab
CMD
expect_ok "whole tui package (older Resume/ExitSave tests stay green)" <<'CMD'
go test -count=1 ./internal/tui
CMD
expect_ok "tab limit constant defined once (T402 reuses it)" <<'CMD'
grep -rnq 'tab limit reached' internal/tui/tabs.go
CMD
expect_ok "tabs and cur fields exist" <<'CMD'
grep -rnq 'tabs \[\]tab' internal/tui/model.go && grep -rnq 'cur ' internal/tui/model.go
CMD
expect_fail "negative: no pointer/alias bug class — internal/agent does not import tui" <<'CMD'
no_match -rn --include='*.go' -e '"yipyip/internal/tui"' internal/agent
CMD
expect_fail "negative: tabs.go never writes files itself (saves route through the T103 save helper via Store)" <<'CMD'
no_match -rn --include='*.go' -e 'os.WriteFile' -e 'os.Create' -e 'json.Marshal' internal/tui/tabs.go
CMD
expect_fail "negative: protected paths untouched (transport + tools at 9519824)" <<'CMD'
out="$(git diff --stat 9519824 -- internal/provider/transport.go internal/tools && git status --porcelain --untracked-files=all -- internal/provider/transport.go internal/tools)" || exit 125
printf '%s' "$out" | no_match -e .
CMD
expect_ok "make check" <<'CMD'
make check
CMD
finish
