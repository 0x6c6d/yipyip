#!/usr/bin/env bash
# Verification for T205 (supervisor-owned; workers never edit it). Exit 0 = pass.
. "$(dirname "$0")/../_lib.sh"
expect_ok "tests: client and SSE parser" <<'CMD'
go_tests ./internal/provider TestRequestPathAndHeaders TestRequestBodyFields TestTextDeltas TestToolCallArgsSplit TestUsageParsing
CMD
expect_ok "transport tests still pass" <<'CMD'
go_tests ./internal/provider TestTransportRejectsForeignHost TestTransportAllowsConfiguredHost
CMD
expect_ok "exported API" <<'CMD'
has_symbols ./internal/provider NewClient Client.Stream Request Message ToolCall ToolDef Usage Event EventText EventToolCall EventUsage EventDone EventError
CMD
expect_ok "make check" <<'CMD'
make check
CMD
expect_fail "negative: the client creates no HTTP client of its own" <<'CMD'
files="$(ls internal/provider/*.go | grep -v -e '_test.go$' -e 'transport.go$')" || exit 125
no_match -nF -e 'http.Client{' -e 'http.DefaultClient' -e 'http.DefaultTransport' -e 'http.Transport{' $files
CMD
finish
