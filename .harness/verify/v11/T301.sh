#!/usr/bin/env bash
# Verification for T301 (supervisor-owned; workers never edit it). Exit 0 = pass.
. "$(dirname "$0")/../_lib.sh"
expect_ok "record mapping tests" <<'CMD'
go_tests ./cmd/yip/ TestRecordTypeFirst TestToolResultTruncated TestRecordRedacted TestResultFields
CMD
expect_ok "mapper and helpers defined" <<'CMD'
grep -q -e 'type mapper struct' cmd/yip/headless_events.go && grep -q -e 'func encodeLine(' cmd/yip/headless_events.go && grep -q -e 'type resultRecord struct' cmd/yip/headless_events.go
CMD
expect_fail "negative: the result record has no cost key" <<'CMD'
no_match -n -e 'json:"cost' cmd/yip/headless_events.go
CMD
expect_ok "cmd/yip tests" <<'CMD'
go test -count=1 ./cmd/yip/
CMD
finish
