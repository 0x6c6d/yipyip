#!/usr/bin/env bash
# Verification for T605 (supervisor-owned; workers never edit it). Exit 0 = pass.
. "$(dirname "$0")/../_lib.sh"
expect_ok "tests: init prompt" <<'CMD'
go_tests ./internal/agent TestInitPromptNew TestInitPromptKeepsExisting
CMD
expect_ok "e2e tests: /init" <<'CMD'
go_tests ./e2e TestInitCreatesAgentsMD TestInitUpdatesExisting TestInitNoModel
CMD
expect_ok "DoD D7: go test ./e2e -run Init" <<'CMD'
go test -count=1 ./e2e -run Init
CMD
expect_ok "exported API" <<'CMD'
has_symbols ./internal/agent InitPrompt Session.Init
CMD
expect_ok "phase 6 exit criterion: go test ./e2e -run 'Init|Compact|Undo'" <<'CMD'
go test -count=1 ./e2e -run 'Init|Compact|Undo'
CMD
expect_ok "make check" <<'CMD'
make check
CMD
finish
