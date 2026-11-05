#!/usr/bin/env bash
# Verification for T406 (supervisor-owned; workers never edit it). Exit 0 = pass.
. "$(dirname "$0")/../_lib.sh"
expect_ok "dir, override and system-append e2e tests" <<'CMD'
go_tests ./e2e/ TestHeadlessDir TestHeadlessDirMissing TestHeadlessOverride TestHeadlessSystemAppend
CMD
expect_ok "earlier headless tests" <<'CMD'
go test -count=1 -run 'Headless' ./e2e/
CMD
expect_fail "negative: the runner never changes the process working directory" <<'CMD'
no_match -rn -e 'os.Chdir' cmd/yip/
CMD
expect_ok "build binary and config" <<'CMD'
set -e
go build -o "$VTMP/yip" ./cmd/yip
mkdir -p "$VTMP/home/.config/yipyip" "$VTMP/work"
printf '%s' '{"default_model":"fake/m1","provider":{"fake":{"name":"Fake","options":{"baseURL":"http://127.0.0.1:9/v1","apiKey":"{env:FAKE_KEY}"},"models":{"m1":{"name":"m1"},"m2":{"name":"m2","reasoning":false}}}}}' > "$VTMP/home/.config/yipyip/config.json"
CMD
expect_ok "snapshot config.json" <<'CMD'
cp -p "$VTMP/home/.config/yipyip/config.json" "$VTMP/config.before"
CMD
expect_fail "negative: missing --dir exits 1" <<'CMD'
cd "$VTMP/work" && env -i PATH="$PATH" HOME="$VTMP/home" FAKE_KEY=SECRET123 "$VTMP/yip" -p hi --dir "$VTMP/nope/x" </dev/null; rc=$?; [ "$rc" -eq 1 ] && exit 1; exit 0
CMD
expect_fail "negative: unknown --model exits 2" <<'CMD'
cd "$VTMP/work" && env -i PATH="$PATH" HOME="$VTMP/home" FAKE_KEY=SECRET123 "$VTMP/yip" -p hi --model nope/x </dev/null; rc=$?; [ "$rc" -eq 2 ] && exit 1; exit 0
CMD
expect_fail "negative: unknown --effort exits 2" <<'CMD'
cd "$VTMP/work" && env -i PATH="$PATH" HOME="$VTMP/home" FAKE_KEY=SECRET123 "$VTMP/yip" -p hi --effort extreme </dev/null; rc=$?; [ "$rc" -eq 2 ] && exit 1; exit 0
CMD
expect_ok "config.json byte-identical after the override attempts" <<'CMD'
cmp "$VTMP/config.before" "$VTMP/home/.config/yipyip/config.json"
CMD
finish
