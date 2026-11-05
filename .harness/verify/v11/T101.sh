#!/usr/bin/env bash
# Verification for T101 (supervisor-owned; workers never edit it). Exit 0 = pass.
. "$(dirname "$0")/../_lib.sh"
expect_ok "headless flag parsing tests" <<'CMD'
go_tests ./cmd/yip/ TestParseHeadlessFlags TestParseHeadlessWithoutPrompt TestParseHeadlessBadValues TestParseInteractiveUnchanged
CMD
expect_ok "existing cmd/yip tests" <<'CMD'
go test -count=1 ./cmd/yip/
CMD
expect_ok "headless_args.go defines headlessFlags" <<'CMD'
grep -q -e 'type headlessFlags struct' cmd/yip/headless_args.go && grep -q -e 'func runHeadless(' cmd/yip/headless.go
CMD
expect_ok "build binary" <<'CMD'
go build -o "$VTMP/yip" ./cmd/yip && mkdir -p "$VTMP/home" "$VTMP/work"
CMD
expect_fail "negative: --json without -p is a usage error (exit 2)" <<'CMD'
cd "$VTMP/work" && env -i PATH="$PATH" HOME="$VTMP/home" "$VTMP/yip" --json </dev/null; rc=$?; [ "$rc" -eq 2 ] && exit 1; exit 0
CMD
expect_fail "negative: empty prompt is a usage error (exit 2)" <<'CMD'
cd "$VTMP/work" && env -i PATH="$PATH" HOME="$VTMP/home" "$VTMP/yip" -p "" </dev/null; rc=$?; [ "$rc" -eq 2 ] && exit 1; exit 0
CMD
expect_fail "negative: --max-model-calls 0 is a usage error (exit 2)" <<'CMD'
cd "$VTMP/work" && env -i PATH="$PATH" HOME="$VTMP/home" "$VTMP/yip" -p hi --max-model-calls 0 </dev/null; rc=$?; [ "$rc" -eq 2 ] && exit 1; exit 0
CMD
expect_ok "--version still works" <<'CMD'
"$VTMP/yip" --version | grep -q -e '^yip '
CMD
finish
