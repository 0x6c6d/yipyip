#!/usr/bin/env bash
# Verification for T303 (supervisor-owned; workers never edit it). Exit 0 = pass.
. "$(dirname "$0")/../_lib.sh"
expect_ok "transcript and secret e2e tests" <<'CMD'
go_tests ./e2e/ TestHeadlessTranscript TestHeadlessTranscriptOutside TestHeadlessSecret
CMD
expect_ok "earlier headless tests" <<'CMD'
go_tests ./e2e/ TestHeadlessJSON TestHeadlessSubagent TestHeadlessPrompt
CMD
expect_ok "transcript path goes through PathGuard Resolve" <<'CMD'
grep -q -e 'NewPathGuard' cmd/yip/headless.go && grep -q -e '\.Resolve(' cmd/yip/headless.go
CMD
expect_ok "build binary and config" <<'CMD'
set -e
go build -o "$VTMP/yip" ./cmd/yip
mkdir -p "$VTMP/home/.config/yipyip" "$VTMP/work"
printf '%s' '{"default_model":"fake/m1","provider":{"fake":{"name":"Fake","options":{"baseURL":"http://127.0.0.1:9/v1","apiKey":"{env:FAKE_KEY}"},"models":{"m1":{"name":"m1"},"m2":{"name":"m2","reasoning":false}}}}}' > "$VTMP/home/.config/yipyip/config.json"
CMD
expect_fail "negative: --transcript ../t.jsonl exits 2 and creates no file" <<'CMD'
cd "$VTMP/work" && env -i PATH="$PATH" HOME="$VTMP/home" FAKE_KEY=SECRET123 "$VTMP/yip" -p hi --transcript ../t.jsonl </dev/null; rc=$?
[ -e "$VTMP/t.jsonl" ] && exit 0
[ "$rc" -eq 2 ] && exit 1; exit 0
CMD
expect_fail "negative: absolute transcript path outside exits 2 and creates no file" <<'CMD'
cd "$VTMP/work" && env -i PATH="$PATH" HOME="$VTMP/home" FAKE_KEY=SECRET123 "$VTMP/yip" -p hi --transcript "$VTMP/abs.jsonl" </dev/null; rc=$?
[ -e "$VTMP/abs.jsonl" ] && exit 0
[ "$rc" -eq 2 ] && exit 1; exit 0
CMD
expect_fail "negative: symlink escape for the transcript exits 2" <<'CMD'
mkdir -p "$VTMP/outside" && ln -sfn "$VTMP/outside" "$VTMP/work/link"
cd "$VTMP/work" && env -i PATH="$PATH" HOME="$VTMP/home" FAKE_KEY=SECRET123 "$VTMP/yip" -p hi --transcript link/t.jsonl </dev/null; rc=$?
[ -e "$VTMP/outside/t.jsonl" ] && exit 0
[ "$rc" -eq 2 ] && exit 1; exit 0
CMD
finish
