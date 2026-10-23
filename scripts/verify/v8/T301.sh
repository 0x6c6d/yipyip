#!/usr/bin/env bash
# Verification for T301 (supervisor-owned; workers never edit it). Exit 0 = pass.
# Probe: scripts/verify/v8/probes/fetch.go (supervisor-owned).
. "$(dirname "$0")/../_lib.sh"
expect_ok "R69: fetch client tests pass" <<'CMD'
go_tests ./internal/provider TestWebFetchBlockedAddresses TestWebFetchRequest TestFetchAddressPolicy TestValidateFetchURL
CMD
expect_ok "provider package passes" <<'CMD'
go test ./internal/provider -count=1
CMD
expect_ok "exported API exists" <<'CMD'
has_symbols ./internal/provider NewFetchHTTPClient FetchOptions ErrAddressNotAllowed ErrTooManyRedirects ErrRedirectNotAllowed ValidateFetchURL FetchTimeout MaxFetchRedirects MaxFetchURLLen
CMD
expect_ok "probe fetch.go builds against the prescribed API" <<'CMD'
build_probe scripts/verify/v8/probes/fetch.go
CMD
expect_ok "probe: defaults (20 s, no jar, own transport, error text)" <<'CMD'
probe_ok defaults
CMD
expect_ok "probe: headers (User-Agent yipyip, no Authorization, no Cookie)" <<'CMD'
probe_ok headers
CMD
expect_ok "probe: 5 redirects are followed" <<'CMD'
probe_ok redirects5
CMD
expect_ok "probe: the Timeout hook bounds a slow server" <<'CMD'
probe_ok timeout
CMD
expect_fail "negative: 6 redirects are refused" <<'CMD'
probe_rejects redirects6
CMD
expect_fail "negative: 127.0.0.1 server without AllowAddr is refused, never reached" <<'CMD'
probe_rejects loopback
CMD
expect_fail "negative: AllowAddr exempts exactly one ip:port" <<'CMD'
probe_rejects allowaddr-port
CMD
expect_fail "negative: redirect to another loopback server is refused at dial" <<'CMD'
probe_rejects redirect-loopback
CMD
expect_fail "negative: redirect target with userinfo is refused" <<'CMD'
probe_rejects redirect-userinfo
CMD
for a in '127.0.0.1:9' '127.1.2.3:9' '[::1]:9' '10.0.0.1:9' '172.16.0.1:9' '192.168.1.1:9' '169.254.169.254:80' '0.0.0.0:9' '0.1.2.3:9' '[::]:9' '[fc00::1]:9' '[fe80::1]:9' '224.0.0.1:9' '[ff02::1]:9' '[::ffff:127.0.0.1]:9' '[::ffff:10.0.0.1]:9' 'localhost:9'; do
	expect_fail "negative: blocked address $a" <<CMD
probe_rejects 'addr=$a'
CMD
done
expect_ok "guard: transport.go loses no code line against a99a82f" <<'CMD'
d="$(git diff a99a82f -- internal/provider/transport.go | grep -E '^-[^-]' | grep -vE '^-\s*//')"
printf '%s\n' "$d"
test -z "$d"
CMD
expect_ok "guard: the address check runs in a dialer Control function" <<'CMD'
grep -e 'Control' internal/provider/transport.go | grep -v -e '^[[:space:]]*//' | grep -q .
CMD
expect_fail "negative: AllowAddr appears in no non-test file except transport.go" <<'CMD'
no_match -rln --include='*.go' --exclude='*_test.go' -e 'AllowAddr' cmd internal --exclude='transport.go'
CMD
expect_fail "negative: no http.Client or http.Transport literal outside transport.go (non-test)" <<'CMD'
no_match -rn --include='*.go' --exclude='*_test.go' --exclude='transport.go' -e 'http\.Client{' -e 'http\.Transport{' -e 'http\.DefaultClient' -e 'http\.DefaultTransport' cmd internal
CMD
expect_ok "pathguard.go, bash.go, redact.go, go.mod, go.sum unchanged since a99a82f" <<'CMD'
git diff --quiet a99a82f -- internal/tools/pathguard.go internal/tools/bash.go internal/config/redact.go go.mod go.sum
CMD
expect_ok "make check" <<'CMD'
make check
CMD
finish
