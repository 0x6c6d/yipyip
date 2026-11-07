#!/usr/bin/env bash
# Verification for T000 (supervisor-owned; workers never edit it). Exit 0 = pass.
. "$(dirname "$0")/../_lib.sh"
expect_ok "go toolchain is 1.27" <<'CMD'
go version | grep -q -e 'go1\.27'
CMD
expect_ok "working tree clean outside .harness/" <<'CMD'
test -z "$(git status --porcelain -- ':!.harness')"
CMD
expect_ok "v13 baseline commit exists" <<'CMD'
git rev-parse --verify -q 81ebe7e^{commit} >/dev/null
CMD
expect_ok "make check passes" <<'CMD'
make check
CMD
finish
