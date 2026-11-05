#!/usr/bin/env bash
# Verification for T601 (supervisor-owned; workers never edit it). Exit 0 = pass.
. "$(dirname "$0")/../_lib.sh"
expect_ok "help tests" <<'CMD'
go_tests ./cmd/yip/ TestHelpFlagHeadless TestHelpFlag TestResumeHelpMentionsFlag
CMD
expect_ok "tui help tests" <<'CMD'
go_tests ./internal/tui/ TestHelpTextHeadless TestHelpTextListsCommands TestHelpTextNoCost
CMD
expect_ok "yip --help lists -p, --json, --answer and exit codes" <<'CMD'
go build -o "$VTMP/yip" ./cmd/yip && "$VTMP/yip" --help >"$VTMP/help" && grep -q -e '-p' "$VTMP/help" && grep -q -e '--json' "$VTMP/help" && grep -q -e '--answer' "$VTMP/help" && grep -q -e '--max-model-calls' "$VTMP/help" && grep -q -e '5 model-call limit' "$VTMP/help"
CMD
expect_fail "negative: help never mentions cost" <<'CMD'
go build -o "$VTMP/yip" ./cmd/yip && "$VTMP/yip" --help | no_match -i -e 'cost'
CMD
finish
