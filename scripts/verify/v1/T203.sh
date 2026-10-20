#!/usr/bin/env bash
# Verification for T203 (supervisor-owned; workers never edit it). Exit 0 = pass.
. "$(dirname "$0")/../_lib.sh"
expect_ok "tests: transport" <<'CMD'
go_tests ./internal/provider TestTransportAllowsConfiguredHost TestTransportRejectsForeignHost TestTransportRejectsOtherPort TestTransportRejectsRedirectToForeignHost TestTransportEmptyAllowlist TestTransportInvalidBaseURL
CMD
expect_ok "make check" <<'CMD'
make check
CMD
expect_ok "probe builds against the prescribed API" <<'CMD'
build_probe scripts/verify/v1/probes/T203.go
CMD
expect_ok "probe: configured host allowed" <<'CMD'
probe_ok allowed
CMD
expect_fail "negative: foreign host refused, never reached" <<'CMD'
probe_rejects foreign
CMD
expect_fail "negative: other host name for the same server refused" <<'CMD'
probe_rejects hostname
CMD
expect_fail "negative: redirect to a foreign host refused" <<'CMD'
probe_rejects redirect
CMD
expect_fail "negative: empty allowlist allows nothing" <<'CMD'
probe_rejects empty
CMD
expect_fail "negative: invalid base URLs refused" <<'CMD'
probe_rejects badurl
CMD
expect_fail "negative: no default client/transport or proxy-from-env in non-test code" <<'CMD'
no_match -rnF -e 'http.DefaultClient' -e 'http.DefaultTransport' -e 'ProxyFromEnvironment' -e 'http.Get(' -e 'http.Post(' -e 'http.Head(' -e 'http.PostForm(' --include='*.go' --exclude='*_test.go' cmd internal
CMD
finish
