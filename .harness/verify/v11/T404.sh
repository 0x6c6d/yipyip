#!/usr/bin/env bash
# Verification for T404 (supervisor-owned; workers never edit it). Exit 0 = pass.
. "$(dirname "$0")/../_lib.sh"
expect_ok "exit precedence unit test" <<'CMD'
go_tests ./cmd/yip/ TestExitFor
CMD
expect_ok "timeout, limit and signal e2e tests (negative: no (n+1)-th request)" <<'CMD'
go_tests ./e2e/ TestHeadlessExit4 TestHeadlessExit5 TestHeadlessExit5Main TestHeadlessSigterm
CMD
expect_ok "runner passes MaxModelCalls" <<'CMD'
grep -q -e 'MaxModelCalls' cmd/yip/headless.go
CMD
expect_ok "build binary and config" <<'CMD'
set -e
go build -o "$VTMP/yip" ./cmd/yip
mkdir -p "$VTMP/home/.config/yipyip" "$VTMP/work"
printf '%s' '{"default_model":"fake/m1","provider":{"fake":{"name":"Fake","options":{"baseURL":"http://127.0.0.1:9/v1","apiKey":"{env:FAKE_KEY}"},"models":{"m1":{"name":"m1"},"m2":{"name":"m2","reasoning":false}}}}}' > "$VTMP/home/.config/yipyip/config.json"
CMD
expect_fail "negative: --timeout 0s exits 2" <<'CMD'
cd "$VTMP/work" && env -i PATH="$PATH" HOME="$VTMP/home" FAKE_KEY=SECRET123 "$VTMP/yip" -p hi --timeout 0s </dev/null; rc=$?; [ "$rc" -eq 2 ] && exit 1; exit 0
CMD
expect_fail "negative: --max-model-calls 0 exits 2" <<'CMD'
cd "$VTMP/work" && env -i PATH="$PATH" HOME="$VTMP/home" FAKE_KEY=SECRET123 "$VTMP/yip" -p hi --max-model-calls 0 </dev/null; rc=$?; [ "$rc" -eq 2 ] && exit 1; exit 0
CMD
finish
