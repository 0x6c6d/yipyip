#!/usr/bin/env bash
# Verification for T403 (supervisor-owned; workers never edit it). Exit 0 = pass.
# Probe: scripts/verify/v8/probes/webfetch.go (supervisor-owned).
. "$(dirname "$0")/../_lib.sh"
expect_ok "R68/R69: web_fetch tool tests pass" <<'CMD'
go_tests ./internal/tools TestWebFetchParams TestWebFetchBlockedAddresses TestWebFetchRequest
CMD
expect_ok "tools package passes (old name-guard tests updated)" <<'CMD'
go test ./internal/tools -count=1
CMD
expect_ok "WebFetchTool constant and Options.Fetch exist" <<'CMD'
has_symbols ./internal/tools WebFetchTool Options.Fetch
CMD
expect_ok "probe webfetch.go builds against the prescribed API" <<'CMD'
build_probe scripts/verify/v8/probes/webfetch.go
CMD
expect_ok "probe: registered always, gated, no web_search without search" <<'CMD'
probe_ok registered
CMD
expect_ok "probe: exact three-part result for a short text/plain page" <<'CMD'
probe_ok exact
CMD
expect_ok "probe: request headers" <<'CMD'
probe_ok headers
CMD
expect_ok "probe: slow server fails after the hook timeout" <<'CMD'
probe_ok slow
CMD
for c in missing empty long ftp file nohost userinfo; do
	expect_fail "negative R68: invalid url ($c) is refused, nothing sent" <<CMD
probe_rejects $c
CMD
done
expect_fail "negative R69: the default client refuses 127.0.0.1" <<'CMD'
probe_rejects default-loopback
CMD
expect_fail "negative R69: redirect to loopback refused" <<'CMD'
probe_rejects redirect-loopback
CMD
expect_fail "negative R69: a secret in the URL is never sent or shown" <<'CMD'
probe_rejects secret
CMD
expect_fail "negative: no HTTP client built in internal/tools (non-test)" <<'CMD'
no_match -rn --include='*.go' --exclude='*_test.go' -e 'http\.Client{' -e 'http\.Transport{' -e 'http\.DefaultClient' -e 'http\.DefaultTransport' -e 'AllowAddr' internal/tools
CMD
expect_ok "name guard exempts exactly web_search and web_fetch" <<'CMD'
grep -q -e 'WebFetchTool' internal/tools/registry.go && grep -q -e 'WebSearchTool' internal/tools/registry.go
CMD
expect_ok "old name-guard tests still pin fetch, curl, http_get, Web_Fetch" <<'CMD'
t="internal/tools/registry_test.go internal/tools/websearch_test.go"
grep -q -F -e '"fetch"' $t && grep -q -F -e '"curl"' $t && grep -q -F -e '"http_get"' $t && grep -q -F -e '"Web_Fetch"' $t
CMD
expect_ok "pathguard.go, bash.go, redact.go, transport.go code, go.mod, go.sum unchanged" <<'CMD'
git diff --quiet a99a82f -- internal/tools/pathguard.go internal/tools/bash.go internal/config/redact.go go.mod go.sum || exit 1
git diff --quiet HEAD -- internal/provider/transport.go internal/agent/approval.go
CMD
expect_ok "make check" <<'CMD'
make check
CMD
finish
