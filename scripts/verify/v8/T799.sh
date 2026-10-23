#!/usr/bin/env bash
# Verification for T799 (supervisor-owned; workers never edit it). Exit 0 = pass.
# Global Definition of Done of versions/v8/plan.md. v4-v7 verify scripts are not rerun (assumption #14).
: "${VERIFY_TIMEOUT:=1200}"
. "$(dirname "$0")/../_lib.sh"

# --- Phase 1 and Phase 2 exits ---
expect_fail "negative AC1: no jev, systemone or typesafe in *.go, *.json, Makefile" <<'CMD'
hits="$(grep -rniE 'jev|systemone|typesafe' --include='*.go' --include='*.json' --include='Makefile' . | grep -v -e '^./scripts/verify/' -e '^./versions/' -e '^./.git/' -e '^./.yipyip/')"
printf '%s\n' "$hits"
test -n "$hits"
CMD
expect_fail "negative AC1: no jev in README.md, docs/usage.md, CLAUDE.md" <<'CMD'
no_match -n -i -e 'jev' README.md docs/usage.md CLAUDE.md
CMD
expect_ok "README sections 1-9 (before ## Usage) unchanged since a99a82f" <<'CMD'
diff <(git show a99a82f:README.md | sed '/^## Usage/,$d') <(sed '/^## Usage/,$d' README.md)
CMD
expect_ok "AC1: go build ./..." <<'CMD'
go build ./...
CMD
expect_ok "§4: TestConfigLeftoverKey passes" <<'CMD'
go_tests ./internal/config TestConfigLeftoverKey
CMD
expect_ok "§4: TestUnknownCommandRemoved passes" <<'CMD'
go_tests ./internal/tui TestUnknownCommandRemoved
CMD

# --- named tests ---
expect_ok "R68-R70: tools WebFetch tests (4 PASS)" <<'CMD'
go_tests ./internal/tools TestWebFetchParams TestWebFetchBlockedAddresses TestWebFetchRequest TestWebFetchResult
CMD
expect_ok "R69: provider WebFetch tests" <<'CMD'
go_tests ./internal/provider TestWebFetchBlockedAddresses TestWebFetchRequest
CMD
expect_ok "AC2/3: go test ./internal/tools ./internal/provider -run WebFetch -v exits 0" <<'CMD'
go test ./internal/tools ./internal/provider -run WebFetch -count=1 -v >/dev/null
CMD
expect_ok "R68/R71: agent WebFetch tests" <<'CMD'
go_tests ./internal/agent TestWebFetchGated TestWebFetchRegistered
CMD
expect_ok "AC5: go test ./e2e -run WebFetch" <<'CMD'
go test ./e2e -run WebFetch -count=1
CMD

# --- supervisor probes: fetch.go (T301) ---
expect_ok "probe fetch.go builds" <<'CMD'
build_probe scripts/verify/v8/probes/fetch.go
CMD
for c in defaults headers redirects5 timeout; do
	expect_ok "probe fetch: $c" <<CMD
probe_ok $c
CMD
done
for c in redirects6 loopback allowaddr-port redirect-loopback redirect-userinfo 'addr=127.0.0.1:9' 'addr=127.1.2.3:9' 'addr=[::1]:9' 'addr=10.0.0.1:9' 'addr=172.16.0.1:9' 'addr=192.168.1.1:9' 'addr=169.254.169.254:80' 'addr=0.0.0.0:9' 'addr=0.1.2.3:9' 'addr=[::]:9' 'addr=[fc00::1]:9' 'addr=[fe80::1]:9' 'addr=224.0.0.1:9' 'addr=[ff02::1]:9' 'addr=[::ffff:127.0.0.1]:9' 'addr=[::ffff:10.0.0.1]:9' 'addr=localhost:9'; do
	expect_fail "negative probe fetch: $c refused" <<CMD
probe_rejects '$c'
CMD
done

# --- supervisor probes: webfetch.go (T403/T404) ---
expect_ok "probe webfetch.go builds" <<'CMD'
build_probe scripts/verify/v8/probes/webfetch.go
CMD
for c in registered html json exact cut cap 404 png headers slow; do
	expect_ok "probe webfetch: $c" <<CMD
probe_ok $c
CMD
done
for c in missing empty long ftp file nohost userinfo default-loopback redirect-loopback secret; do
	expect_fail "negative probe webfetch: $c refused" <<CMD
probe_rejects $c
CMD
done

# --- supervisor probes: agentfetch.go (T501) ---
expect_ok "probe agentfetch.go builds" <<'CMD'
build_probe scripts/verify/v8/probes/agentfetch.go
CMD
for c in auto manual-allow; do
	expect_ok "probe agentfetch: $c" <<CMD
probe_ok $c
CMD
done
for c in manual-deny default-client; do
	expect_fail "negative probe agentfetch: $c refused" <<CMD
probe_rejects $c
CMD
done

# --- diff guards ---
expect_ok "R71/R7: approval.go diff = exactly one + line \"web_fetch\": true, and no - line" <<'CMD'
d="$(git diff -U0 a99a82f -- internal/agent/approval.go | grep -E '^[-+][^-+]')"
printf '%s\n' "$d"
test "$(printf '%s\n' "$d" | grep -c .)" -eq 1 && printf '%s\n' "$d" | grep -q -E '^\+[[:space:]]*"web_fetch":[[:space:]]+true,$'
CMD
expect_ok "R69/AC6: transport.go removes no non-comment line since a99a82f" <<'CMD'
d="$(git diff a99a82f -- internal/provider/transport.go | grep -E '^-[^-]' | grep -vE '^-\s*//')"
printf '%s\n' "$d"
test -z "$d"
CMD
expect_ok "AC6: pathguard.go, bash.go, redact.go, go.mod, go.sum unchanged since a99a82f" <<'CMD'
git diff --quiet a99a82f -- internal/tools/pathguard.go internal/tools/bash.go internal/config/redact.go go.mod go.sum
CMD
expect_ok "Delegation workflow section of CLAUDE.md equals 5a884db" <<'CMD'
diff <(git show 5a884db:CLAUDE.md | sed -n '/^## Delegation workflow/,$p') <(sed -n '/^## Delegation workflow/,$p' CLAUDE.md)
CMD
expect_ok "R69: AllowAddr in non-test Go code only in internal/provider/transport.go" <<'CMD'
l="$(grep -rln --include='*.go' --exclude='*_test.go' AllowAddr cmd internal)"
printf '%s\n' "$l"
test "$l" = "internal/provider/transport.go"
CMD
expect_fail "negative: no http.Client{ / http.Transport{ literal outside transport.go (non-test)" <<'CMD'
no_match -rn --include='*.go' --exclude='*_test.go' --exclude='transport.go' -e 'http\.Client{' -e 'http\.Transport{' cmd internal
CMD
expect_fail "negative: no http.DefaultClient, DefaultTransport, http.Get/Post/Head in non-test code" <<'CMD'
no_match -rn --include='*.go' --exclude='*_test.go' -e 'http\.DefaultClient' -e 'http\.DefaultTransport' -e 'http\.Get(' -e 'http\.Post(' -e 'http\.Head(' cmd internal
CMD

# --- Phase 6 exit, secrets, make check ---
expect_ok "R72: web_fetch in docs/usage.md and at least twice in CLAUDE.md" <<'CMD'
grep -q web_fetch docs/usage.md && test "$(grep -c web_fetch CLAUDE.md)" -ge 2
CMD
expect_ok "secrets: go test ./... -run Secret" <<'CMD'
go test ./... -run Secret -count=1
CMD
expect_ok "AC7: make check" <<'CMD'
make check
CMD
finish
