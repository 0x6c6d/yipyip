#!/usr/bin/env bash
# Verification for T000 (supervisor-owned; workers never edit it). Exit 0 = pass.
. "$(dirname "$0")/../_lib.sh"
expect_ok "Linux host" <<'CMD'
test "$(uname -s)" = Linux
CMD
expect_ok "/proc readable" <<'CMD'
test -r /proc/self/stat
CMD
expect_ok "bash, setsid, sleep on PATH" <<'CMD'
command -v bash && command -v setsid && command -v sleep
CMD
expect_ok "clean working tree (except .harness/)" <<'CMD'
test -z "$(git status --porcelain -- . ':(exclude).harness')"
CMD
expect_ok "baseline make check" <<'CMD'
make check
CMD
finish
