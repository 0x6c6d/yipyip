#!/usr/bin/env bash
# Verification for T000 (supervisor-owned; workers never edit it). Exit 0 = pass.
. "$(dirname "$0")/../_lib.sh"
expect_ok "go 1.27 installed" <<'CMD'
go version | grep -q -e 'go1\.2[7-9]'
CMD
expect_ok "code tree clean (cmd internal e2e Makefile go.mod go.sum)" <<'CMD'
test -z "$(git status --porcelain -- cmd internal e2e Makefile go.mod go.sum)"
CMD
expect_ok "baseline commit 4fabda9 exists" <<'CMD'
git rev-parse -q --verify '4fabda9^{commit}'
CMD
expect_ok "go.mod and go.sum unchanged since 4fabda9" <<'CMD'
git diff --quiet 4fabda9 -- go.mod go.sum
CMD
expect_ok "fake provider package present" <<'CMD'
test -d internal/provider/providertest
CMD
expect_ok "offline build works (no new modules needed)" <<'CMD'
GOPROXY=off GOFLAGS=-mod=readonly go build ./...
CMD
expect_ok "make check on the v3 baseline" <<'CMD'
make check
CMD
expect_fail "negative: an unknown commit id is rejected" <<'CMD'
git rev-parse -q --verify 'deadbeefdeadbeefdeadbeefdeadbeefdeadbeef^{commit}'
CMD
expect_fail "negative: internal/skills does not exist before T101" <<'CMD'
test -e internal/skills
CMD
finish
