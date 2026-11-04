#!/usr/bin/env bash
# Verification for T799 (supervisor-owned; workers never edit it). Exit 0 = pass.
. "$(dirname "$0")/../_lib.sh"
expect_ok "internal/session DoD suite" <<'CMD'
go test -count=1 ./internal/session
CMD
expect_ok "internal/tui DoD scoped groups" <<'CMD'
go test -count=1 ./internal/tui
CMD
expect_ok "internal/provider Retry suite" <<'CMD'
go test -count=1 ./internal/provider
CMD
expect_ok "internal/config Settings suite" <<'CMD'
go test -count=1 ./internal/config
CMD
expect_ok "e2e DoD suites" <<'CMD'
go test -count=1 ./e2e
CMD
expect_ok "secret suite repo-wide" <<'CMD'
go test ./... -run Secret
CMD
expect_ok "--resume greps: cmd flag and atomic session write" <<'CMD'
grep -nq -- '--resume' cmd/yip/main.go && grep -rnq -- 'os.Rename' internal/session
CMD
expect_fail "no openrouter.ai outside _test.go (no_match exits 1 when nothing is found)" <<'CMD'
grep -rn --include='*.go' -e 'openrouter\.ai' . | grep -v '^.*_test\.go:' | no_match -e .
CMD
expect_fail "negative: protected path diff (transport + tools) empty at 9519824" <<'CMD'
git rev-parse -q --verify '9519824^{commit}' >/dev/null || exit 125
out="$(git diff --stat 9519824 -- internal/provider/transport.go internal/tools && git status --porcelain --untracked-files=all -- internal/provider/transport.go internal/tools)" || exit 125
printf '%s' "$out" | no_match -e .
CMD
expect_ok "make check" <<'CMD'
make check
CMD
finish
