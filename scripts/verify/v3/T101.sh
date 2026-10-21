#!/usr/bin/env bash
# Verification for T101 (supervisor-owned; workers never edit it). Exit 0 = pass.
. "$(dirname "$0")/../_lib.sh"
expect_ok "session store tests incl. negatives" <<'CMD'
go_tests ./internal/session TestSaveLoadRoundTrip TestSaveAtomicNoTempLeftovers TestListNewestFirst TestDelete TestLoadVersionMismatch TestLoadInvalidJSON TestLoadUnknownFieldsIgnored TestSymlinkedRefused TestNoSecretInFile
CMD
expect_ok "whole internal/session package" <<'CMD'
go test -count=1 ./internal/session
CMD
expect_ok "store writes atomically (os.Rename present)" <<'CMD'
grep -rnq -- 'os.Rename' internal/session/store.go
CMD
expect_ok ".gitignore ignores sessions dir" <<'CMD'
grep -qF -e '.yipyip/' .gitignore
CMD
expect_fail "negative: the parts store is absent" <<'CMD'
grep -rnF -e '"parts"' internal/session
CMD
expect_fail "negative: internal/agent never imports internal/session" <<'CMD'
no_match -rn --include='*.go' -e '"yipyip/internal/session"' internal/agent
CMD
expect_fail "negative: no http client or exec in internal/session" <<'CMD'
no_match -rn --include='*.go' -e 'http.Get' -e 'http.Post' -e 'http.DefaultClient' -e 'exec.Command' -e 'sh -c' internal/session
CMD
expect_fail "negative: protected paths untouched (transport + tools at 9519824)" <<'CMD'
out="$(git diff --stat 9519824 -- internal/provider/transport.go internal/tools && git status --porcelain --untracked-files=all -- internal/provider/transport.go internal/tools)" || exit 125
printf '%s' "$out" | no_match -e .
CMD
expect_fail "negative: no protected path touched" <<'CMD'
out="$(git status --porcelain --untracked-files=all -- README.md Plan.md prompts .claude scripts/verify versions/v3/spec.md versions/v3/plan.md versions/v3/tasks versions/v3/Tasks.md internal/provider/transport.go internal/tools)" || exit 125
printf '%s' "$out" | no_match -e .
CMD
expect_ok "make check" <<'CMD'
make check
CMD
finish
