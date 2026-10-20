#!/usr/bin/env bash
# Verification for T204 (supervisor-owned; workers never edit it). Exit 0 = pass.
. "$(dirname "$0")/../_lib.sh"
expect_ok "tests: fake provider" <<'CMD'
go_tests ./internal/provider/providertest TestFakeTextStream TestFakeToolCallSplit TestFakeRecordsRequests TestFakeErrorStatus TestFakeDrop TestFakeDefaultAfterScript TestConfigJSON
CMD
expect_ok "make check" <<'CMD'
make check
CMD
expect_ok "probe builds against the prescribed API" <<'CMD'
build_probe scripts/verify/v1/probes/T204.go
CMD
expect_ok "probe: text stream, usage chunk, recorded request" <<'CMD'
probe_ok text
CMD
expect_ok "probe: tool-call arguments split across chunks" <<'CMD'
probe_ok toolsplit
CMD
expect_ok "probe: error status" <<'CMD'
probe_ok status
CMD
expect_ok "probe: default reply after the script" <<'CMD'
probe_ok default
CMD
expect_ok "probe: dropped stream has no [DONE]" <<'CMD'
probe_ok drop
CMD
expect_ok "probe: NewFunc" <<'CMD'
probe_ok func
CMD
expect_ok "probe: ConfigJSON" <<'CMD'
probe_ok config
CMD
expect_fail "negative: providertest imports no yipyip package" <<'CMD'
out="$(go list -f '{{join .Imports "\n"}}' ./internal/provider/providertest)" || exit 125
printf '%s\n' "$out" | no_match '^yipyip/'
CMD
finish
