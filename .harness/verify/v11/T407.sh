#!/usr/bin/env bash
# Verification for T407 (supervisor-owned; workers never edit it). Exit 0 = pass.
. "$(dirname "$0")/../_lib.sh"
expect_ok "exit code e2e tests" <<'CMD'
go_tests ./e2e/ TestHeadlessExit0 TestHeadlessExit1 TestHeadlessExit2
CMD
expect_ok "cmd/yip tests" <<'CMD'
go test -count=1 ./cmd/yip/
CMD
expect_ok "build binary and config" <<'CMD'
set -e
go build -o "$VTMP/yip" ./cmd/yip
mkdir -p "$VTMP/home/.config/yipyip" "$VTMP/work"
printf '%s' '{"default_model":"fake/m1","provider":{"fake":{"name":"Fake","options":{"baseURL":"http://127.0.0.1:9/v1","apiKey":"{env:FAKE_KEY}"},"models":{"m1":{"name":"m1"},"m2":{"name":"m2","reasoning":false}}}}}' > "$VTMP/home/.config/yipyip/config.json"
CMD
expect_ok "usage error with --json prints one result line with reason usage" <<'CMD'
cd "$VTMP/work" && env -i PATH="$PATH" HOME="$VTMP/home" FAKE_KEY=SECRET123 "$VTMP/yip" -p x --timeout bogus --json </dev/null >"$VTMP/out" 2>/dev/null; rc=$?
[ "$rc" -eq 2 ] && [ "$(wc -l <"$VTMP/out")" -eq 1 ] && grep -q -e '^{"type":"result"' "$VTMP/out" && grep -q -e '"reason":"usage"' "$VTMP/out"
CMD
expect_fail "negative: usage error without --json writes nothing to stdout" <<'CMD'
cd "$VTMP/work" && env -i PATH="$PATH" HOME="$VTMP/home" FAKE_KEY=SECRET123 "$VTMP/yip" -p x --timeout bogus </dev/null >"$VTMP/out2" 2>/dev/null; rc=$?
[ "$rc" -eq 2 ] && [ ! -s "$VTMP/out2" ] && exit 1; exit 0
CMD
finish
