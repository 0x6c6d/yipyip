#!/usr/bin/env bash
# Verification for T404 (supervisor-owned; workers never edit it). Exit 0 = pass.
# Probe: scripts/verify/v8/probes/webfetch.go (supervisor-owned).
. "$(dirname "$0")/../_lib.sh"
expect_ok "R70: TestWebFetchResult passes" <<'CMD'
go_tests ./internal/tools TestWebFetchResult
CMD
expect_ok "Phase 4 exit: tools package passes and exactly four TestWebFetch* PASS lines" <<'CMD'
go test ./internal/tools -count=1 || exit 1
n="$(go test ./internal/tools -run WebFetch -count=1 -v | grep -cE '^--- PASS: TestWebFetch(Params|BlockedAddresses|Request|Result) ')"
echo "PASS lines: $n"
test "$n" = 4
CMD
expect_ok "probe webfetch.go builds" <<'CMD'
build_probe scripts/verify/v8/probes/webfetch.go
CMD
expect_ok "probe: HTML becomes clean text" <<'CMD'
probe_ok html
CMD
expect_ok "probe: JSON is kept as is" <<'CMD'
probe_ok json
CMD
expect_ok "probe: exact three-part result" <<'CMD'
probe_ok exact
CMD
expect_ok "probe: 30,000 characters cut to 20,000 + truncated" <<'CMD'
probe_ok cut
CMD
expect_ok "negative R70: nothing beyond 1 MiB is read (probe cap)" <<'CMD'
probe_ok cap
CMD
expect_ok "negative R70: 404 gives exactly 'web_fetch failed: HTTP 404'" <<'CMD'
probe_ok 404
CMD
expect_ok "negative R70: image/png gives exactly 'web_fetch failed: unsupported content type'" <<'CMD'
probe_ok png
CMD
expect_fail "negative R69 (regression): the default client still refuses 127.0.0.1" <<'CMD'
probe_rejects default-loopback
CMD
expect_fail "negative R69 (regression): a secret in the URL is still never sent or shown" <<'CMD'
probe_rejects secret
CMD
expect_fail "negative: invalid url still refused (userinfo)" <<'CMD'
probe_rejects userinfo
CMD
expect_ok "guard: transport.go, approval.go, pathguard.go, bash.go, go.mod, go.sum untouched by this task" <<'CMD'
git diff --quiet HEAD -- internal/provider/transport.go internal/agent/approval.go internal/tools/pathguard.go internal/tools/bash.go go.mod go.sum
CMD
expect_ok "make check" <<'CMD'
make check
CMD
finish
