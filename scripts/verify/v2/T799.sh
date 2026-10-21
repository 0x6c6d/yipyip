#!/usr/bin/env bash
# Verification for T799 (supervisor-owned; workers never edit it). Exit 0 = pass.
. "$(dirname "$0")/../_lib.sh"
# Global Definition of Done of versions/v2/plan.md, run by the supervisor.
expect_ok "D1 every v2 test name matches at least one test" <<'CMD'
for n in Status Suggest History Copy Paste Scroll Render Thinking Finished Subagent Queue Mode Help; do
  go test ./internal/tui -list "$n" | grep -q -e '^Test' || { echo "no test matches $n"; exit 1; }
done
CMD
expect_ok "D1 v2 TUI tests pass" <<'CMD'
go test -count=1 ./internal/tui -run 'Status|Suggest|History|Copy|Paste|Scroll|Render|Thinking|Finished|Subagent|Queue|Mode|Help'
CMD
expect_ok "D2 auto mode and mouse capture in cmd/yip" <<'CMD'
grep -n -e 'SetMode(agent.ModeAuto)' cmd/yip/main.go && grep -n -e 'WithMouseCellMotion' cmd/yip/main.go
CMD
expect_ok "D3 yip --help lists the new keys" <<'CMD'
out="$(go run ./cmd/yip --help)" || exit 1
for k in 'Up' 'Ctrl+Home' 'Ctrl+End' 'Tab'; do printf '%s\n' "$out" | grep -qF -e "$k" || { echo "missing $k"; exit 1; }; done
printf '%s\n' "$out" | grep -qi -e 'queue'
CMD
expect_ok "D4 Secret tests repo-wide (incl. copy test)" <<'CMD'
go test -count=1 ./... -run Secret && go test ./internal/tui -list Secret | grep -q -e '^TestCopySecretNeverCopied$'
CMD
expect_fail "D5 negative: agent, tools, provider, config unchanged since v1" <<'CMD'
git rev-parse -q --verify 'd3eeba4^{commit}' >/dev/null || exit 125
out="$(git diff --stat d3eeba4 -- internal/agent internal/tools internal/provider internal/config && git status --porcelain --untracked-files=all -- internal/agent internal/tools internal/provider internal/config)" || exit 125
printf '%s' "$out" | no_match -e .
CMD
expect_ok "D6 exec and clipboard imports only in bash.go and clipboard.go" <<'CMD'
got="$(grep -rln --include='*.go' --exclude='*_test.go' -e '"os/exec"' -e 'atotto/clipboard' cmd internal e2e | sort | tr '\n' ' ')"
[ "$got" = "internal/tools/bash.go internal/tui/clipboard.go " ] || { echo "got: $got"; exit 1; }
CMD
expect_fail "D7 negative: README.md, prompts, .claude, shared lib and v1 scripts unchanged since v1" <<'CMD'
out="$(git diff --stat d3eeba4 -- README.md prompts .claude scripts/verify/_lib.sh scripts/verify/v1 && git status --porcelain --untracked-files=all -- README.md prompts .claude scripts/verify)" || exit 125
printf '%s' "$out" | no_match -e .
CMD
expect_ok "D8 make check" <<'CMD'
make check
CMD
finish
