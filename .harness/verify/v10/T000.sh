#!/usr/bin/env bash
# Verification for T000 (supervisor-owned; workers never edit it). Exit 0 = pass.
. "$(dirname "$0")/../_lib.sh"
. "$(dirname "$0")/_probe.sh"
expect_ok "Go toolchain is 1.27" <<'CMD'
go version | grep -q -e 'go1\.27'
CMD
expect_ok "v10 start commit 1ccbbc7 exists" <<'CMD'
git cat-file -e 1ccbbc7
CMD
expect_ok "no uncommitted changes in cmd, internal, e2e, go.mod, go.sum" <<'CMD'
test -z "$(git status --porcelain -- cmd internal e2e go.mod go.sum)"
CMD
expect_ok "make check" <<'CMD'
make check
CMD
finish
