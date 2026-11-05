#!/usr/bin/env bash
# Verification for T102 (supervisor-owned; workers never edit it). Exit 0 = pass.
. "$(dirname "$0")/../_lib.sh"
expect_ok "headless e2e skeleton tests" <<'CMD'
go_tests ./e2e/ TestHeadlessPrompt TestHeadlessStdin TestHeadlessUsage TestHeadlessToolProgress
CMD
expect_ok "cmd/yip tests" <<'CMD'
go test -count=1 ./cmd/yip/
CMD
expect_fail "negative: headless.go imports no Bubble Tea" <<'CMD'
no_match -n -e 'bubbletea' cmd/yip/headless.go
CMD
expect_fail "negative: headless.go never saves settings" <<'CMD'
no_match -n -e 'SaveSettings' cmd/yip/headless.go
CMD
expect_ok "build binary and config" <<'CMD'
set -e
go build -o "$VTMP/yip" ./cmd/yip
mkdir -p "$VTMP/home/.config/yipyip" "$VTMP/work"
printf '%s' '{"default_model":"fake/m1","provider":{"fake":{"name":"Fake","options":{"baseURL":"http://127.0.0.1:9/v1","apiKey":"{env:FAKE_KEY}"},"models":{"m1":{"name":"m1"},"m2":{"name":"m2","reasoning":false}}}}}' > "$VTMP/home/.config/yipyip/config.json"
CMD
expect_fail "negative: -p - with empty stdin exits 2" <<'CMD'
cd "$VTMP/work" && printf '' | env -i PATH="$PATH" HOME="$VTMP/home" FAKE_KEY=SECRET123 "$VTMP/yip" -p -; rc=$?; [ "$rc" -eq 2 ] && exit 1; exit 0
CMD
expect_fail "negative: -p /model exits 2 with one stderr line" <<'CMD'
cd "$VTMP/work" && env -i PATH="$PATH" HOME="$VTMP/home" FAKE_KEY=SECRET123 "$VTMP/yip" -p /model </dev/null >"$VTMP/out" 2>"$VTMP/err"; rc=$?
[ "$rc" -eq 2 ] && [ "$(grep -c . "$VTMP/err")" -eq 1 ] && ! grep -q SECRET123 "$VTMP/err" && exit 1; exit 0
CMD
finish
