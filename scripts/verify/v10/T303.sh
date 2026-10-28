#!/usr/bin/env bash
# Verification for T303 (supervisor-owned; workers never edit it). Exit 0 = pass.
. "$(dirname "$0")/../_lib.sh"
. "$(dirname "$0")/_probe.sh"
expect_ok "tests" <<'CMD'
go_tests ./cmd/yip TestStartupPruneKeeps25 TestStartupPruneProtectsResumed TestStartupPruneIgnoresStrayFiles TestStartupPruneNoSessionsDir
CMD
expect_ok "built binary with --resume of an old id keeps it (probe via the launch seam test)" <<'CMD'
go test -count=1 ./cmd/yip -run 'TestStartupPruneProtectsResumed' -v | grep -q -e '--- PASS'
CMD
expect_fail "negative: runUI prunes before applyResume (the resumed session could be deleted before it is loaded)" <<'CMD'
awk '/^func runUI/{f=1} f && /applyResume\(/ && !r{r=NR} f && /[pP]rune/ && !p{p=NR} END{exit !(p && r && p < r)}' cmd/yip/main.go
CMD
expect_fail "negative: Prune must not be called from the UI package at startup" <<'CMD'
no_match -n -e 'Prune(' internal/tui/model.go
CMD
expect_ok "security files, go.mod and go.sum unchanged since 1ccbbc7" <<'CMD'
v10_guard
CMD
expect_ok "make check" <<'CMD'
make check
CMD
finish
