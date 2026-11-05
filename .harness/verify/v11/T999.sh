#!/usr/bin/env bash
# Verification for T999 (supervisor-owned; workers never edit it). Exit 0 = pass.
# Global Definition of Done of .harness/versions/v11/plan.md.
. "$(dirname "$0")/../_lib.sh"
expect_ok "all headless e2e tests" <<'CMD'
go test -count=1 -run Headless ./e2e/
CMD
expect_ok "DoD headless tests exist and pass by name" <<'CMD'
go_tests ./e2e/ TestHeadlessPrompt TestHeadlessStdin TestHeadlessSkill TestHeadlessSlashModel TestHeadlessJSON TestHeadlessSubagent TestHeadlessExit0 TestHeadlessExit1 TestHeadlessExit2 TestHeadlessExit3 TestHeadlessExit4 TestHeadlessExit5 TestHeadlessResume TestHeadlessNoSave TestHeadlessTranscriptOutside TestHeadlessAnswerOrder TestHeadlessApprove TestHeadlessDir TestHeadlessOverride TestHeadlessSystemAppend TestHeadlessSecret
CMD
expect_ok "paste display test" <<'CMD'
go_tests ./internal/tui/ TestPasteShown
CMD
expect_ok "agent and session seam tests" <<'CMD'
go_tests ./internal/agent/ TestSystemAppend TestModelCallLimitMain TestModelCallLimitSubagents && go_tests ./internal/session/ TestFromAgent
CMD
expect_ok "README check of this version" <<'CMD'
bash .harness/verify/v11/T602.sh
CMD
expect_ok "yip --help lists -p, --json, --answer" <<'CMD'
go build -o "$VTMP/yip" ./cmd/yip && "$VTMP/yip" --help >"$VTMP/help" && grep -q -e '-p' "$VTMP/help" && grep -q -e '--json' "$VTMP/help" && grep -q -e '--answer' "$VTMP/help"
CMD
expect_ok "README documents --max-model-calls" <<'CMD'
test "$(grep -c -- '--max-model-calls' README.md)" -ge 1
CMD
expect_ok "build binary and config" <<'CMD'
set -e
go build -o "$VTMP/yip" ./cmd/yip
mkdir -p "$VTMP/home/.config/yipyip" "$VTMP/work"
printf '%s' '{"default_model":"fake/m1","provider":{"fake":{"name":"Fake","options":{"baseURL":"http://127.0.0.1:9/v1","apiKey":"{env:FAKE_KEY}"},"models":{"m1":{"name":"m1"},"m2":{"name":"m2","reasoning":false}}}}}' > "$VTMP/home/.config/yipyip/config.json"
CMD
expect_fail "negative: --json without -p exits 2" <<'CMD'
cd "$VTMP/work" && env -i PATH="$PATH" HOME="$VTMP/home" "$VTMP/yip" --json </dev/null; rc=$?; [ "$rc" -eq 2 ] && exit 1; exit 0
CMD
expect_fail "negative: transcript outside the start directory is refused" <<'CMD'
cd "$VTMP/work" && env -i PATH="$PATH" HOME="$VTMP/home" FAKE_KEY=SECRET123 "$VTMP/yip" -p hi --transcript ../t.jsonl </dev/null; rc=$?
[ -e "$VTMP/t.jsonl" ] && exit 0
[ "$rc" -eq 2 ] && exit 1; exit 0
CMD
expect_ok "make check" <<'CMD'
make check
CMD
finish
