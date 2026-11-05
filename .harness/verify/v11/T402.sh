#!/usr/bin/env bash
# Verification for T402 (supervisor-owned; workers never edit it). Exit 0 = pass.
. "$(dirname "$0")/../_lib.sh"
expect_ok "answer and question exit e2e tests (incl. negative: unanswered question)" <<'CMD'
go_tests ./e2e/ TestHeadlessAnswerOrder TestHeadlessExit3 TestHeadlessExit3Extra
CMD
expect_ok "earlier headless tests" <<'CMD'
go_tests ./e2e/ TestHeadlessJSON TestHeadlessSubagent TestHeadlessPrompt TestHeadlessSecret
CMD
expect_ok "cmd/yip tests" <<'CMD'
go test -count=1 ./cmd/yip/
CMD
expect_fail "negative: the runner never answers a question with an empty fallback" <<'CMD'
no_match -n -e 'Respond("")' cmd/yip/headless.go
CMD
finish
