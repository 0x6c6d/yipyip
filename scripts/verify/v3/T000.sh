#!/usr/bin/env bash
# Verification for T000 (supervisor-owned; workers never edit it). Exit 0 = pass.
. "$(dirname "$0")/../_lib.sh"
expect_ok "go 1.27 installed" <<'CMD'
go version | grep -q -e 'go1\.27'
CMD
expect_ok "working tree clean" <<'CMD'
test -z "$(git status --porcelain)"
CMD
expect_ok "baseline commit 9519824 exists" <<'CMD'
git rev-parse -q --verify '9519824^{commit}'
CMD
expect_ok "protected paths unchanged since 9519824" <<'CMD'
out="$(git diff --stat 9519824..HEAD -- internal/provider/transport.go internal/tools)" && test -z "$out"
CMD
expect_ok "offline build works (no new modules needed)" <<'CMD'
GOPROXY=off GOFLAGS=-mod=readonly go build ./...
CMD
expect_ok "make check on the v2 baseline" <<'CMD'
make check
CMD
expect_fail "negative: an unknown or unrelated commit id is rejected" <<'CMD'
git rev-parse -q --verify 'deadbeefdeadbeefdeadbeefdeadbeefdeadbeef^{commit}'
CMD
finish
