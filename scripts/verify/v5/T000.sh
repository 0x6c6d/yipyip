#!/usr/bin/env bash
# Verification for T000 (supervisor-owned; workers never edit it). Exit 0 = pass.
. "$(dirname "$0")/../_lib.sh"
expect_ok "go 1.27 installed" <<'CMD'
go version | grep -q -e 'go1\.2[7-9]'
CMD
expect_ok "v5 spec, v5 plan and Plan.md present" <<'CMD'
test -f versions/v5/spec.md && test -f versions/v5/plan.md && test -f Plan.md
CMD
expect_ok "code tree clean (cmd internal e2e Makefile go.mod go.sum)" <<'CMD'
test -z "$(git status --porcelain -- cmd internal e2e Makefile go.mod go.sum)"
CMD
expect_ok "baseline commit 0c3a7c5 exists" <<'CMD'
git rev-parse -q --verify '0c3a7c5^{commit}'
CMD
expect_ok "go.mod and go.sum unchanged since 0c3a7c5" <<'CMD'
git diff --quiet 0c3a7c5 -- go.mod go.sum
CMD
expect_ok "fake provider and fake System One present" <<'CMD'
test -d internal/provider/providertest && grep -q -e 'func NewJev' internal/provider/providertest/jev.go
CMD
expect_ok "offline build works (no new modules needed)" <<'CMD'
GOPROXY=off GOFLAGS=-mod=readonly go build ./...
CMD
expect_ok "make check on the v4 baseline" <<'CMD'
make check
CMD
expect_fail "negative: an unknown commit id is rejected" <<'CMD'
git rev-parse -q --verify 'deadbeefdeadbeefdeadbeefdeadbeefdeadbeef^{commit}'
CMD
expect_fail "negative: MaxSubagentIterations does not exist before T401" <<'CMD'
no_match -rn -e 'MaxSubagentIterations' internal
CMD
finish
