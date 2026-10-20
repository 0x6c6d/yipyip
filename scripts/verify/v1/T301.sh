#!/usr/bin/env bash
# Verification for T301 (supervisor-owned; workers never edit it). Exit 0 = pass.
. "$(dirname "$0")/../_lib.sh"
expect_ok "tests: PathGuard" <<'CMD'
go_tests ./internal/tools TestGuardRelative TestGuardAbsoluteInside TestGuardDotDotEscape TestGuardAbsoluteOutside TestGuardSymlinkEscape TestGuardSymlinkInside TestGuardNewFile TestGuardDanglingSymlink TestGuardRootSymlinked
CMD
expect_ok "make check" <<'CMD'
make check
CMD
expect_ok "probe builds against the prescribed API" <<'CMD'
build_probe scripts/verify/v1/probes/T301.go
CMD
expect_ok "probe: inside paths (relative, absolute, symlinked root, new files, in-root links)" <<'CMD'
probe_ok inside
CMD
expect_fail "negative: .. escapes rejected" <<'CMD'
probe_rejects dotdot
CMD
expect_fail "negative: absolute outside paths rejected" <<'CMD'
probe_rejects abs
CMD
expect_fail "negative: symlinked file/dir escapes rejected" <<'CMD'
probe_rejects symlink
CMD
expect_fail "negative: new file under an outward symlink rejected" <<'CMD'
probe_rejects symlinknew
CMD
expect_fail "negative: dangling symlink rejected" <<'CMD'
probe_rejects dangling
CMD
expect_fail "negative: empty path rejected" <<'CMD'
probe_rejects empty
CMD
finish
