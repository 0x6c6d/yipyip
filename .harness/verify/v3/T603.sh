#!/usr/bin/env bash
# Verification for T603 (supervisor-owned; workers never edit it). Exit 0 = pass.
. "$(dirname "$0")/../_lib.sh"
expect_ok "newline tests" <<'CMD'
go_tests ./internal/tui TestShiftEnterInsertsNewline TestAltEnterUnchanged TestHelpMentionsShiftEnter TestHelpShiftEnterOneLine
CMD
expect_ok "cmd/yip help test" <<'CMD'
go_tests ./cmd/yip TestHelpFlagMentionsShiftEnter
CMD
expect_ok "both packages still green" <<'CMD'
go test -count=1 ./internal/tui && go test -count=1 ./cmd/yip
CMD
expect_ok "shift+enter added to the newline binding in addition to alt+enter (same line)" <<'CMD'
grep -rn 'alt+enter' internal/tui/model.go | grep -q 'shift+enter'
CMD
expect_ok "insert-newline binding itself carries shift+enter" <<'CMD'
f=internal/tui/model.go
sed -n '/InsertNewline/,/)/p' $f | grep -q 'shift+enter'
CMD
expect_ok "help text mentions both keys" <<'CMD'
grep -rnq 'Shift+Enter' internal/tui/help.go && grep -rn 'Alt+Enter' internal/tui/help.go | grep -q 'Shift+Enter'
CMD
expect_ok "alt+enter row still there (now sharing the shift+enter row)" <<'CMD'
grep -rn 'Alt+Enter' internal/tui/help.go | grep -q 'Shift+Enter'
CMD
expect_ok "make check" <<'CMD'
make check
CMD
finish
