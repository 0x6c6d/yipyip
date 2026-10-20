#!/usr/bin/env bash
# Verification for T103 (supervisor-owned; workers never edit it). Exit 0 = pass.
. "$(dirname "$0")/../_lib.sh"
expect_ok "tests: loader" <<'CMD'
go_tests ./internal/config TestLoadGlobal TestProjectOverride TestLoadMissingFiles TestLoadInvalidJSON TestMergeDoesNotMutate
CMD
expect_ok "exported API" <<'CMD'
has_symbols ./internal/config Load Merge GlobalPath ProjectFile Config Config.DefaultModel Config.MaxSubagents Config.AutoCompact Config.Raw
CMD
expect_ok "make check" <<'CMD'
make check
CMD
expect_fail "negative: the config package writes no files" <<'CMD'
no_match -rnF -e 'os.WriteFile' -e 'os.Create' -e 'os.OpenFile' -e 'os.Mkdir' --include='*.go' --exclude='*_test.go' internal/config
CMD
finish
