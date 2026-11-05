#!/usr/bin/env bash
# Verification for T000 (supervisor-owned; workers never edit it). Exit 0 = pass.
. "$(dirname "$0")/../_lib.sh"
expect_ok "go toolchain is 1.27" <<'CMD'
go version | grep -q 'go1.27'
CMD
expect_ok "working tree is clean" <<'CMD'
test -z "$(git status --porcelain)"
CMD
expect_ok "make check passes" <<'CMD'
make check
CMD
finish
