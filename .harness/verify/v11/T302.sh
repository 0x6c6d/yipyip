#!/usr/bin/env bash
# Verification for T302 (supervisor-owned; workers never edit it). Exit 0 = pass.
. "$(dirname "$0")/../_lib.sh"
expect_ok "JSON and subagent e2e tests" <<'CMD'
go_tests ./e2e/ TestHeadlessJSON TestHeadlessSubagent
CMD
expect_ok "earlier headless tests" <<'CMD'
go_tests ./e2e/ TestHeadlessPrompt TestHeadlessStdin TestHeadlessUsage TestHeadlessToolProgress
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
expect_fail "negative: --json run with a config error exits 1, stdout only JSON, no secret" <<'CMD'
cd "$VTMP/work" && env -i PATH="$PATH" HOME="$VTMP/home" "$VTMP/yip" -p hi --json </dev/null >"$VTMP/out" 2>"$VTMP/err"; rc=$?
[ "$rc" -eq 1 ] || exit 0
while IFS= read -r line; do case "$line" in '{"type":'*) ;; *) exit 0;; esac; done <"$VTMP/out"
exit 1
CMD
finish
