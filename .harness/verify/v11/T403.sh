#!/usr/bin/env bash
# Verification for T403 (supervisor-owned; workers never edit it). Exit 0 = pass.
. "$(dirname "$0")/../_lib.sh"
expect_ok "approve e2e tests" <<'CMD'
go_tests ./e2e/ TestHeadlessApprove TestHeadlessApproveUnknown TestHeadlessAutoNoApproval TestHeadlessManualOtherToolDenied
CMD
expect_ok "Session.ToolNames exists" <<'CMD'
has_symbols ./internal/agent Session.ToolNames
CMD
expect_ok "agent and cmd/yip tests" <<'CMD'
go test -count=1 ./internal/agent/ ./cmd/yip/
CMD
expect_fail "negative: the runner never grants AllowAlways" <<'CMD'
no_match -rn -e 'AllowAlways' cmd/yip/
CMD
expect_ok "approval.go untouched by this task" <<'CMD'
git diff --quiet HEAD -- internal/agent/approval.go
CMD
expect_ok "build binary and config" <<'CMD'
set -e
go build -o "$VTMP/yip" ./cmd/yip
mkdir -p "$VTMP/home/.config/yipyip" "$VTMP/work"
printf '%s' '{"default_model":"fake/m1","provider":{"fake":{"name":"Fake","options":{"baseURL":"http://127.0.0.1:9/v1","apiKey":"{env:FAKE_KEY}"},"models":{"m1":{"name":"m1"},"m2":{"name":"m2","reasoning":false}}}}}' > "$VTMP/home/.config/yipyip/config.json"
CMD
expect_fail "negative: --approve nosuchtool exits 2" <<'CMD'
cd "$VTMP/work" && env -i PATH="$PATH" HOME="$VTMP/home" FAKE_KEY=SECRET123 "$VTMP/yip" -p hi --mode manual --approve nosuchtool </dev/null; rc=$?; [ "$rc" -eq 2 ] && exit 1; exit 0
CMD
expect_fail "negative: --mode bogus exits 2" <<'CMD'
cd "$VTMP/work" && env -i PATH="$PATH" HOME="$VTMP/home" FAKE_KEY=SECRET123 "$VTMP/yip" -p hi --mode bogus </dev/null; rc=$?; [ "$rc" -eq 2 ] && exit 1; exit 0
CMD
finish
