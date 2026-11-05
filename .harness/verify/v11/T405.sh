#!/usr/bin/env bash
# Verification for T405 (supervisor-owned; workers never edit it). Exit 0 = pass.
. "$(dirname "$0")/../_lib.sh"
expect_ok "session e2e tests" <<'CMD'
go_tests ./e2e/ TestHeadlessResume TestHeadlessNoSave TestHeadlessSaveOnQuestion TestHeadlessPrune
CMD
expect_ok "earlier headless and retention tests" <<'CMD'
go test -count=1 -run 'Headless|Retention|Session' ./e2e/
CMD
expect_ok "runner uses the shared session helpers" <<'CMD'
grep -q -e 'session.FromAgent(' cmd/yip/headless.go && grep -q -e 'session.ApplyTo(' cmd/yip/headless.go && grep -q -e '\.Prune(' cmd/yip/headless.go
CMD
expect_ok "build binary and config" <<'CMD'
set -e
go build -o "$VTMP/yip" ./cmd/yip
mkdir -p "$VTMP/home/.config/yipyip" "$VTMP/work"
printf '%s' '{"default_model":"fake/m1","provider":{"fake":{"name":"Fake","options":{"baseURL":"http://127.0.0.1:9/v1","apiKey":"{env:FAKE_KEY}"},"models":{"m1":{"name":"m1"},"m2":{"name":"m2","reasoning":false}}}}}' > "$VTMP/home/.config/yipyip/config.json"
CMD
expect_fail "negative: unknown --resume id exits 1 with 'unknown session <id>'" <<'CMD'
cd "$VTMP/work" && env -i PATH="$PATH" HOME="$VTMP/home" FAKE_KEY=SECRET123 "$VTMP/yip" -p hi --resume abcdefabcdef </dev/null 2>"$VTMP/err"; rc=$?
[ "$rc" -eq 1 ] && grep -q -e 'unknown session abcdefabcdef' "$VTMP/err" && exit 1; exit 0
CMD
expect_fail "negative: --resume without id with -p exits 2" <<'CMD'
cd "$VTMP/work" && env -i PATH="$PATH" HOME="$VTMP/home" FAKE_KEY=SECRET123 "$VTMP/yip" -p hi --resume </dev/null; rc=$?; [ "$rc" -eq 2 ] && exit 1; exit 0
CMD
expect_fail "negative: a usage error with --no-save creates no sessions dir" <<'CMD'
cd "$VTMP/work" && env -i PATH="$PATH" HOME="$VTMP/home" FAKE_KEY=SECRET123 "$VTMP/yip" -p /model --no-save </dev/null; rc=$?
[ -e "$VTMP/work/.yipyip/sessions" ] && exit 0
[ "$rc" -eq 2 ] && exit 1; exit 0
CMD
finish
