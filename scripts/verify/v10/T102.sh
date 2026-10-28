#!/usr/bin/env bash
# Verification for T102 (supervisor-owned; workers never edit it). Exit 0 = pass.
. "$(dirname "$0")/../_lib.sh"
. "$(dirname "$0")/_probe.sh"
expect_ok "cmd/yip tests" <<'CMD'
go_tests ./cmd/yip TestMainCrashLaunchPanic TestMainCrashMainPanic TestMainCrashProgramPanicFallback TestMainCrashWriteFailureStderr TestMainNoCrashNoLog
CMD
expect_ok "tui guard tests" <<'CMD'
go_tests ./internal/tui TestGuardUpdatePanic TestGuardViewPanic TestGuardCmdPanic TestGuardPassThrough
CMD
expect_ok "built binary, testPanic=launch: exit 2, crash.log record, stderr line" <<'CMD'
d="$VTMP/bin-launch"; mkdir -p "$d/home/.config/yipyip" "$d/start"
cat >"$d/home/.config/yipyip/config.json" <<'JSON'
{"default_model":"fake/m1","provider":{"fake":{"options":{"baseURL":"http://127.0.0.1:9/v1","apiKey":"{env:FAKE_KEY}"},"models":{"m1":{}}}}}
JSON
go build -ldflags "-X main.version=probe -X main.testPanic=launch" -o "$d/yip" ./cmd/yip
cd "$d/start" && HOME="$d/home" FAKE_KEY=SECRET123 "$d/yip" </dev/null >"$d/out" 2>"$d/err"; echo $? >"$d/code"; true
test "$(cat $d/code)" = 2 && grep -q -e '^yip: crashed, details in ' "$d/err" && grep -q -e 'panic: forced test panic' "$d/start/.yipyip/crash.log" && grep -q -e 'version: probe' "$d/start/.yipyip/crash.log" && grep -q -e '^stack:' "$d/start/.yipyip/crash.log"
CMD
expect_ok "built binary, testPanic=main: exit 2 and a record" <<'CMD'
d="$VTMP/bin-main"; mkdir -p "$d/home/.config/yipyip" "$d/start"
cat >"$d/home/.config/yipyip/config.json" <<'JSON'
{"default_model":"fake/m1","provider":{"fake":{"options":{"baseURL":"http://127.0.0.1:9/v1","apiKey":"{env:FAKE_KEY}"},"models":{"m1":{}}}}}
JSON
go build -ldflags "-X main.version=probe -X main.testPanic=main" -o "$d/yip" ./cmd/yip
cd "$d/start" && HOME="$d/home" FAKE_KEY=SECRET123 "$d/yip" </dev/null >"$d/out" 2>"$d/err"; echo $? >"$d/code"; true
test "$(cat $d/code)" = 2 && test -s "$d/start/.yipyip/crash.log"
CMD
expect_fail "negative: testPanic=leak must never write the configured secret (it must be absent)" <<'CMD'
d="$VTMP/bin-leak"; mkdir -p "$d/home/.config/yipyip" "$d/start"
cat >"$d/home/.config/yipyip/config.json" <<'JSON'
{"default_model":"fake/m1","provider":{"fake":{"options":{"baseURL":"http://127.0.0.1:9/v1","apiKey":"{env:FAKE_KEY}"},"models":{"m1":{}}}}}
JSON
go build -ldflags "-X main.version=probe -X main.testPanic=leak" -o "$d/yip" ./cmd/yip
cd "$d/start" && HOME="$d/home" FAKE_KEY=SECRET123 "$d/yip" </dev/null >"$d/out" 2>"$d/err"; echo $? >"$d/code"; true
test "$(cat $d/code)" = 2 || exit 125
grep -q -e SECRET123 "$d/start/.yipyip/crash.log" "$d/err"
CMD
expect_fail "negative: yip --version leaves a crash.log" <<'CMD'
d="$VTMP/ver"; mkdir -p "$d"; go build -ldflags "-X main.version=probe" -o "$d/yip" ./cmd/yip && cd "$d" && ./yip --version >/dev/null && test -e .yipyip/crash.log
CMD
expect_fail "negative: the shipped code never reads an env var for the panic seam" <<'CMD'
no_match -n -e 'YIP_TEST_PANIC' -e 'Getenv("YIPYIP_PANIC' cmd/yip/main.go cmd/yip/crash.go
CMD
expect_ok "tea.WithoutCatchPanics is not used (Bubble Tea restores the terminal)" <<'CMD'
! grep -rn "WithoutCatchPanics" cmd internal --include=*.go | grep -v _test.go
CMD
expect_ok "security files, go.mod and go.sum unchanged since 1ccbbc7" <<'CMD'
v10_guard
CMD
expect_ok "make check" <<'CMD'
make check
CMD
finish
