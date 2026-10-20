#!/usr/bin/env bash
# Verification for T401 (supervisor-owned; workers never edit it). Exit 0 = pass.
. "$(dirname "$0")/../_lib.sh"
expect_ok "tests: approval gate" <<'CMD'
go_tests ./internal/agent TestGateManualPromptsGated TestGateReadsNeverPrompt TestGateDeny TestGateAlwaysPerTool TestGateAutoNeverPrompts TestGateReset TestGateCancel TestGateLabel
CMD
expect_ok "make check" <<'CMD'
make check
CMD
expect_ok "probe builds against the prescribed API" <<'CMD'
build_probe scripts/verify/v1/probes/T401.go
CMD
expect_ok "probe: reads and auto mode never prompt; IsGated" <<'CMD'
probe_ok free
CMD
expect_ok "probe: always is per tool, y not remembered, Reset forgets" <<'CMD'
probe_ok always
CMD
expect_ok "probe: request labelled with the agent id; first answer wins" <<'CMD'
probe_ok label
CMD
expect_fail "negative: denied bash is not allowed" <<'CMD'
probe_rejects deny
CMD
expect_fail "negative: unanswered prompt ends with an error on ctx cancel" <<'CMD'
probe_rejects cancel
CMD
finish
