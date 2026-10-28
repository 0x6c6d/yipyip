#!/usr/bin/env bash
# Verification for T301 (supervisor-owned; workers never edit it). Exit 0 = pass.
. "$(dirname "$0")/../_lib.sh"
. "$(dirname "$0")/_probe.sh"
expect_ok "tests" <<'CMD'
go_tests ./internal/session TestPruneKeepsNewest TestPruneProtect TestPruneStrayFilesUntouched TestPruneInvalidJSONNotCounted TestPruneMissingDir TestPruneKeepNotPositive TestPruneSymlinkEscape TestPruneUnderKeepNoop
CMD
expect_ok "Prune exists" <<'CMD'
has_symbols ./internal/session Store.Prune
CMD
expect_ok "probe builds" <<'CMD'
build_probe scripts/verify/v10/probes/prune.go
CMD
expect_ok "probe: 30 files, keep 25 + 2 protected, stray files untouched" <<'CMD'
probe_ok keep
CMD
expect_fail "negative: keep=0 deletes nothing and is an error" <<'CMD'
probe_rejects zero
CMD
expect_fail "negative: a symlinked sessions dir escaping the start dir is refused" <<'CMD'
probe_rejects escape
CMD
expect_fail "negative: Prune never removes files with os.Remove outside the store helper" <<'CMD'
no_match -n -e 'os.RemoveAll' internal/session/store.go
CMD
expect_ok "security files, go.mod and go.sum unchanged since 1ccbbc7" <<'CMD'
v10_guard
CMD
expect_ok "make check" <<'CMD'
make check
CMD
finish
