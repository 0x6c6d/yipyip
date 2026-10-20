#!/usr/bin/env bash
# Verification for T402 (supervisor-owned; workers never edit it). Exit 0 = pass.
. "$(dirname "$0")/../_lib.sh"
expect_ok "tests: session core" <<'CMD'
go_tests ./internal/agent TestNewSessionDefaults TestSetModelUnknown TestSetEffort TestToggleMode TestSystemPromptAgentsMD TestEventsNeverBlock TestResetClearsState TestReasoningDisabled
CMD
expect_ok "make check" <<'CMD'
make check
CMD
expect_ok "probe builds against the prescribed API" <<'CMD'
build_probe scripts/verify/v1/probes/T402.go
CMD
expect_ok "probe: defaults, setters, system prompt with AGENTS.md" <<'CMD'
probe_ok defaults
CMD
expect_ok "probe: reset" <<'CMD'
probe_ok reset
CMD
expect_fail "negative: unknown model refused, model unchanged" <<'CMD'
probe_rejects badmodel
CMD
expect_fail "negative: invalid effort refused, effort unchanged" <<'CMD'
probe_rejects badeffort
CMD
finish
