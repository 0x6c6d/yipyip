#!/usr/bin/env bash
# Verification for T206 (supervisor-owned; workers never edit it). Exit 0 = pass.
. "$(dirname "$0")/../_lib.sh"
expect_ok "tests: provider errors" <<'CMD'
go_tests ./internal/provider TestHTTP500 TestHTTP401 TestDroppedStream TestMalformedChunk TestCancelStream TestRefusedHostError TestSecretProviderError
CMD
expect_ok "client tests still pass" <<'CMD'
go_tests ./internal/provider TestRequestBodyFields TestToolCallArgsSplit TestUsageParsing
CMD
expect_ok "make check" <<'CMD'
make check
CMD
expect_ok "probe builds against the prescribed API" <<'CMD'
build_probe scripts/verify/v1/probes/T206.go
CMD
expect_ok "probe: normal stream ends with EventDone" <<'CMD'
probe_ok ok
CMD
expect_fail "negative: 401 echoing the key yields a redacted error (nil Redactor)" <<'CMD'
probe_rejects leak401
CMD
expect_fail "negative: dropped stream is an error" <<'CMD'
probe_rejects drop
CMD
expect_fail "negative: malformed chunk is an error" <<'CMD'
probe_rejects malformed
CMD
expect_fail "negative: foreign host refused, never reached, no key in the error" <<'CMD'
probe_rejects foreign
CMD
expect_fail "negative: cancel ends the stream with context.Canceled within 2 s" <<'CMD'
probe_rejects cancel
CMD
finish
