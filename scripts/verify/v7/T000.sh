#!/usr/bin/env bash
# Verification for T000 (supervisor-owned; workers never edit it). Exit 0 = pass.
. "$(dirname "$0")/../_lib.sh"
expect_ok "go toolchain is go1.27" <<'CMD'
go version | grep -q -e ' go1\.27'
CMD
expect_ok "working tree is clean" <<'CMD'
test -z "$(git status --porcelain)"
CMD
expect_ok "main agent unlimited in loop.go (const MaxIterations = 0)" <<'CMD'
grep -n -e 'const MaxIterations = 0' internal/agent/loop.go
CMD
expect_ok "every v7 verify script exists and is executable" <<'CMD'
for id in T000 T101 T102 T201 T202 T203 T204 T299; do test -x "scripts/verify/v7/$id.sh" && test -f "versions/v7/tasks/$id.md" || { echo "missing: $id"; exit 1; }; done
CMD
expect_fail "negative: no old main limit constant" <<'CMD'
no_match -n -e 'const MaxIterations = 50' internal/agent/loop.go
CMD
expect_ok "make check" <<'CMD'
make check
CMD
finish
