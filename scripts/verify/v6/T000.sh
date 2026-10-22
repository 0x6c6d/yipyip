#!/usr/bin/env bash
# Verification for T000 (supervisor-owned; workers never edit it). Exit 0 = pass.
. "$(dirname "$0")/../_lib.sh"
expect_ok "go 1.27 installed" <<'CMD'
go version | grep -q -e 'go1\.2[7-9]'
CMD
expect_ok "v6 spec, v6 plan and Plan.md present" <<'CMD'
test -f versions/v6/spec.md && test -f versions/v6/plan.md && test -f Plan.md
CMD
expect_ok "code tree clean (cmd internal e2e docs README.md Makefile go.mod go.sum)" <<'CMD'
test -z "$(git status --porcelain -- cmd internal e2e docs README.md Makefile go.mod go.sum)"
CMD
expect_ok "baseline commit ede3886 exists" <<'CMD'
git rev-parse -q --verify 'ede3886^{commit}'
CMD
expect_ok "go.mod and go.sum unchanged since ede3886" <<'CMD'
git diff --quiet ede3886 -- go.mod go.sum
CMD
expect_ok "security-critical files present" <<'CMD'
test -f internal/provider/transport.go && test -f internal/tools/pathguard.go && test -f internal/agent/approval.go && test -f internal/tools/bash.go && test -f internal/config/redact.go
CMD
expect_ok "fake provider and fake System One present" <<'CMD'
test -d internal/provider/providertest && grep -q -e 'func NewJev' internal/provider/providertest/jev.go
CMD
expect_ok "offline build works (no new modules needed)" <<'CMD'
GOPROXY=off GOFLAGS=-mod=readonly go build ./...
CMD
expect_ok "make check on the v5 baseline" <<'CMD'
make check
CMD
expect_fail "negative: an unknown commit id is rejected" <<'CMD'
git rev-parse -q --verify 'deadbeefdeadbeefdeadbeefdeadbeefdeadbeef^{commit}'
CMD
expect_fail "negative: transport.go already mentions search (baseline not fresh)" <<'CMD'
no_match -n -i -e 'search' internal/provider/transport.go
CMD
expect_fail "negative: approval.go already gates web_search" <<'CMD'
no_match -n -e 'web_search' internal/agent/approval.go
CMD
expect_fail "negative: ResolveRead already exists" <<'CMD'
no_match -rn -e 'ResolveRead' internal
CMD
finish
