#!/usr/bin/env bash
# Verification for T304 (supervisor-owned; workers never edit it). Exit 0 = pass.
. "$(dirname "$0")/../_lib.sh"
expect_ok "tests: file tools" <<'CMD'
go_tests ./internal/tools TestReadFile TestReadFileOutside TestWriteFileCreatesDirs TestWriteFileOutsideRejected TestEditFileMissing TestEditFileAmbiguous TestEditFileOK TestBeforeWriteHook
CMD
expect_ok "make check" <<'CMD'
make check
CMD
expect_ok "probe builds against the prescribed API" <<'CMD'
build_probe scripts/verify/v1/probes/T304.go
CMD
expect_ok "probe: read/write/edit inside, gating, BeforeWrite" <<'CMD'
probe_ok ok
CMD
expect_fail "negative: .. and absolute escapes refused, nothing created outside" <<'CMD'
probe_rejects escape
CMD
expect_fail "negative: symlink escapes refused for read, write and edit" <<'CMD'
probe_rejects symlink
CMD
expect_fail "negative: ambiguous/missing/empty old_string refused, file untouched" <<'CMD'
probe_rejects edit
CMD
finish
