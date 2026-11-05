#!/usr/bin/env bash
# Verification for T201 (supervisor-owned; workers never edit it). Exit 0 = pass.
. "$(dirname "$0")/../_lib.sh"
expect_ok "SystemAppend tests" <<'CMD'
go_tests ./internal/agent/ TestSystemAppend TestSystemAppendAfterReset TestSystemAppendEmpty
CMD
expect_ok "Options.SystemAppend exists" <<'CMD'
go doc ./internal/agent Options | grep -q -e 'SystemAppend'
CMD
expect_ok "whole agent package" <<'CMD'
go test -count=1 ./internal/agent/
CMD
finish
