#!/usr/bin/env bash
# Verification for T502 (supervisor-owned; workers never edit it). Exit 0 = pass.
. "$(dirname "$0")/../_lib.sh"
expect_ok "restore tests incl. negative nothing-sent test" <<'CMD'
go_tests ./internal/tui TestQueueEscRestores TestQueueCtrlCRestores TestQueueUpRestores TestQueueUpEmptyQueueHistory TestQueueClearedByNew TestQueueNothingSentAfterInterrupt
CMD
expect_ok "v1 cancel and quit tests" <<'CMD'
go_tests ./internal/tui TestEscCancels TestDoubleCtrlCQuits
CMD
expect_ok "Queue, History, Finished tests (3 runs)" <<'CMD'
go test -count=3 ./internal/tui -run 'Queue|History|Finished'
CMD
expect_fail "negative: no file-writing call in non-test internal/tui code" <<'CMD'
no_match -rn --include='*.go' --exclude='*_test.go' -e 'os.WriteFile' -e 'os.Create' -e 'os.OpenFile' -e 'ioutil.WriteFile' -e 'os.MkdirAll' internal/tui
CMD
expect_fail "negative: no protected path touched" <<'CMD'
out="$(git status --porcelain --untracked-files=all -- README.md Plan.md prompts .claude scripts/verify versions/v2/spec.md versions/v2/plan.md versions/v2/tasks)" || exit 125
printf '%s' "$out" | no_match -e .
CMD
expect_fail "negative: agent, tools, provider, config unchanged since v1 (d3eeba4)" <<'CMD'
git rev-parse -q --verify 'd3eeba4^{commit}' >/dev/null || exit 125
out="$(git diff d3eeba4 -- internal/agent internal/tools internal/provider internal/config && git status --porcelain --untracked-files=all -- internal/agent internal/tools internal/provider internal/config)" || exit 125
printf '%s' "$out" | no_match -e .
CMD
expect_ok "make check" <<'CMD'
make check
CMD
finish
