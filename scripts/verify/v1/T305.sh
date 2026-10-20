#!/usr/bin/env bash
# Verification for T305 (supervisor-owned; workers never edit it). Exit 0 = pass.
. "$(dirname "$0")/../_lib.sh"
expect_ok "tests: glob and grep" <<'CMD'
go_tests ./internal/tools TestGlobDoubleStar TestMatchGlob TestGlobSkipsGit TestGrepFormat TestGrepGlobFilter TestGrepSkipsBinary TestGrepInvalidRegex TestSearchPathEscape
CMD
expect_ok "phase 3 exit criterion: go test ./internal/tools" <<'CMD'
go test -count=1 ./internal/tools
CMD
expect_ok "make check" <<'CMD'
make check
CMD
expect_ok "probe builds against the prescribed API" <<'CMD'
build_probe scripts/verify/v1/probes/T305.go
CMD
expect_ok "probe: glob with **" <<'CMD'
probe_ok globstar
CMD
expect_ok "probe: grep path:line:text, .git and binary skipped, glob filter" <<'CMD'
probe_ok format
CMD
expect_fail "negative: search paths outside the start dir refused" <<'CMD'
probe_rejects escape
CMD
expect_fail "negative: invalid regex refused" <<'CMD'
probe_rejects badregex
CMD
finish
