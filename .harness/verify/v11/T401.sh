#!/usr/bin/env bash
# Verification for T401 (supervisor-owned; workers never edit it). Exit 0 = pass.
. "$(dirname "$0")/../_lib.sh"
expect_ok "skill and slash e2e tests" <<'CMD'
go_tests ./e2e/ TestHeadlessSkill TestHeadlessSlashModel TestHeadlessSlashUnknown TestHeadlessSkillShadowed
CMD
expect_ok "tui.IsBuiltinCommand exists" <<'CMD'
has_symbols ./internal/tui IsBuiltinCommand
CMD
expect_ok "tui and cmd/yip tests" <<'CMD'
go test -count=1 ./internal/tui/ ./cmd/yip/
CMD
expect_ok "build binary and config" <<'CMD'
set -e
go build -o "$VTMP/yip" ./cmd/yip
mkdir -p "$VTMP/home/.config/yipyip" "$VTMP/work"
printf '%s' '{"default_model":"fake/m1","provider":{"fake":{"name":"Fake","options":{"baseURL":"http://127.0.0.1:9/v1","apiKey":"{env:FAKE_KEY}"},"models":{"m1":{"name":"m1"},"m2":{"name":"m2","reasoning":false}}}}}' > "$VTMP/home/.config/yipyip/config.json"
CMD
expect_fail "negative: -p /agents exits 2 with exactly one stderr line" <<'CMD'
cd "$VTMP/work" && env -i PATH="$PATH" HOME="$VTMP/home" FAKE_KEY=SECRET123 "$VTMP/yip" -p /agents </dev/null >"$VTMP/out" 2>"$VTMP/err"; rc=$?
[ "$rc" -eq 2 ] && [ "$(grep -c . "$VTMP/err")" -eq 1 ] && grep -q -e '/agents' "$VTMP/err" && exit 1; exit 0
CMD
finish
