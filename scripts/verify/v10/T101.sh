#!/usr/bin/env bash
# Verification for T101 (supervisor-owned; workers never edit it). Exit 0 = pass.
. "$(dirname "$0")/../_lib.sh"
. "$(dirname "$0")/_probe.sh"
expect_ok "tests" <<'CMD'
go_tests ./internal/crashlog TestCrashlogRecordFormat TestCrashlogRecoveredMarked TestCrashlogSessionID TestCrashlogPerms TestCrashlogCap TestCrashlogCapSingleHugeRecord TestCrashlogRedacts TestCrashlogResolveError TestCrashlogNoDefaultNoop TestCrashlogConcurrent
CMD
expect_ok "race detector" <<'CMD'
go test -race -count=1 ./internal/crashlog
CMD
expect_ok "exported API exists" <<'CMD'
has_symbols ./internal/crashlog New Options Logger.Fatal Logger.Recovered Logger.Path SetDefault Default Recovered MaxBytes KeepBytes
CMD
expect_ok "only the standard library is imported (no yipyip package)" <<'CMD'
test -z "$(go list -deps ./internal/crashlog | grep -e '^yipyip/' | grep -v -x -e yipyip/internal/crashlog)"
CMD
expect_ok "probe builds against the prescribed API" <<'CMD'
build_probe scripts/verify/v10/probes/crashlog.go
CMD
expect_ok "probe: record format, kinds, where, session, stack" <<'CMD'
probe_ok format
CMD
expect_ok "probe: dir 0700 when created, file 0600" <<'CMD'
probe_ok perms
CMD
expect_ok "probe: cap at 1 MiB cuts to 512 KiB, newest record kept" <<'CMD'
probe_ok cap
CMD
expect_fail "negative: a registered secret in the panic value must not reach crash.log" <<'CMD'
probe_rejects leak
CMD
expect_fail "negative: a failing Resolve writes nothing to disk" <<'CMD'
probe_rejects escape
CMD
expect_ok "make check" <<'CMD'
make check
CMD
finish
