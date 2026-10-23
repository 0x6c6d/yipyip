#!/usr/bin/env bash
# Verification for T501 (supervisor-owned; workers never edit it). Exit 0 = pass.
# Probe: scripts/verify/v8/probes/agentfetch.go (supervisor-owned).
. "$(dirname "$0")/../_lib.sh"
expect_ok "R68/R71: TestWebFetchGated and TestWebFetchRegistered pass" <<'CMD'
go_tests ./internal/agent TestWebFetchGated TestWebFetchRegistered
CMD
expect_ok "agent package passes" <<'CMD'
go test ./internal/agent -count=1
CMD
expect_ok "Options.FetchClient exists" <<'CMD'
has_symbols ./internal/agent Options.FetchClient
CMD
expect_ok "FetchClient goes to tools.Options.Fetch" <<'CMD'
grep -n -E 'Fetch:[[:space:]]+opts\.FetchClient' internal/agent/session.go
CMD
expect_fail "negative: the provider client is never the fetch client" <<'CMD'
no_match -n -E -e 'Fetch:[[:space:]]+(opts\.HTTPClient|hc|s\.hc|shc)\b' internal/agent/session.go
CMD
expect_ok "probe agentfetch.go builds against the prescribed API" <<'CMD'
build_probe scripts/verify/v8/probes/agentfetch.go
CMD
expect_ok "probe: auto mode runs web_fetch without a prompt" <<'CMD'
probe_ok auto
CMD
expect_ok "probe: manual mode prompts with the URL; allow fetches once" <<'CMD'
probe_ok manual-allow
CMD
expect_fail "negative R71: manual deny sends no request" <<'CMD'
probe_rejects manual-deny
CMD
expect_fail "negative R69: the production client (no seam) refuses 127.0.0.1" <<'CMD'
probe_rejects default-client
CMD
expect_ok "guard: approval.go diff is exactly the one web_fetch line" <<'CMD'
d="$(git diff -U0 a99a82f -- internal/agent/approval.go | grep -E '^[-+][^-+]')"
printf '%s\n' "$d"
test "$(printf '%s\n' "$d" | grep -c .)" -eq 1 && printf '%s\n' "$d" | grep -q -E '^\+[[:space:]]*"web_fetch":[[:space:]]+true,$'
CMD
expect_ok "scope: only internal/agent changed, approval.go untouched by this task" <<'CMD'
git diff --quiet HEAD -- internal/agent/approval.go || exit 1
changed="$( { git diff --name-only HEAD; git ls-files -o --exclude-standard; } | sort -u | grep -v -e '^versions/v8/Tasks.md$')"
printf '%s\n' "$changed"
test -z "$(printf '%s\n' "$changed" | grep -v -e '^$' -e '^internal/agent/')"
CMD
expect_ok "make check" <<'CMD'
make check
CMD
finish
