#!/usr/bin/env bash
# Verification for T000 (supervisor-owned; workers never edit it). Exit 0 = pass.
. "$(dirname "$0")/../_lib.sh"
expect_ok "go 1.27 installed" <<'CMD'
go version | grep -q -e 'go1\.27'
CMD
expect_ok "working tree clean" <<'CMD'
test -z "$(git status --porcelain)"
CMD
expect_ok "baseline commit d3eeba4 exists" <<'CMD'
git rev-parse -q --verify 'd3eeba4^{commit}'
CMD
expect_ok "clipboard modules already in go.sum" <<'CMD'
grep -q -e 'atotto/clipboard v0.1.4' go.sum && grep -q -e 'go-osc52/v2 v2.0.1' go.sum
CMD
expect_ok "offline build works" <<'CMD'
GOPROXY=off GOFLAGS=-mod=readonly go build ./...
CMD
expect_ok "make check on the v1 baseline" <<'CMD'
make check
CMD
expect_fail "negative: an unknown commit id is rejected" <<'CMD'
git rev-parse -q --verify 'deadbeefdeadbeefdeadbeefdeadbeefdeadbeef^{commit}'
CMD
finish
