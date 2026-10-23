#!/usr/bin/env bash
# Verification for T299 (supervisor-owned; workers never edit it). Exit 0 = pass.
# Global Definition of Done of versions/v7/plan.md.
: "${VERIFY_TIMEOUT:=1200}"
. "$(dirname "$0")/../_lib.sh"
expect_ok "R64: TestMainNoIterationLimit passes and const MaxIterations = 0" <<'CMD'
go test ./internal/agent -run 'MainNoIterationLimit' -count=1 && grep -n -e 'const MaxIterations = 0' internal/agent/loop.go
CMD
expect_ok "R65: --- PASS: TestMainCancelLongTurn" <<'CMD'
out="$(go test ./internal/agent -run 'MainNoIterationLimit|MainCancel' -count=1 -v 2>&1)" || { printf '%s\n' "$out" | tail -n 40; exit 1; }
printf '%s\n' "$out" | grep -e '^--- PASS: TestMainCancelLongTurn '
CMD
expect_ok "R54: TestSubagentIterationLimit passes, const MaxSubagentIterations = 500" <<'CMD'
go test ./internal/agent -run SubagentIterationLimit -count=1 && grep -n -e 'const MaxSubagentIterations = 500' internal/agent/loop.go
CMD
expect_fail "negative R66: no main-50 phrase in docs/usage.md or CLAUDE.md" <<'CMD'
no_match -n -E -e 'main agent stops after 50|50 model calls per main turn|iteration limit \(50\)' docs/usage.md CLAUDE.md
CMD
expect_ok "R66: docs/usage.md says 'no limit'" <<'CMD'
grep -n -e 'no limit' docs/usage.md
CMD
expect_ok "R67: scripts/verify/v5/T401.sh exits 0" <<'CMD'
bash scripts/verify/v5/T401.sh
CMD
expect_ok "R67: scripts/verify/v5/T799.sh exits 0" <<'CMD'
bash scripts/verify/v5/T799.sh
CMD
expect_fail "negative R67: no main-50 names or assertions in scripts/verify/v5" <<'CMD'
no_match -n -F -e 'TestMainIterationLimit' -e 'TestZZPosMain50' -e 'MaxIterations != 50' scripts/verify/v5/*.sh
CMD
expect_ok "R6: v1 T403.sh fails at most on the 'probe: HTTP error shown' line" <<'CMD'
out="$(bash scripts/verify/v1/T403.sh 2>&1)"
printf '%s\n' "$out" | grep -q -e '^-- [0-9]* passed' || { printf '%s\n' "$out" | tail -n 20; exit 1; }
other="$(printf '%s\n' "$out" | grep -e '^FAIL' | grep -v -e 'probe: HTTP error shown')"
test -z "$other" || { printf '%s\n' "$other"; exit 1; }
CMD
expect_fail "negative R6: old R6 phrases are gone from README.md" <<'CMD'
no_match -n -e 'iteration limit stops at 50' -e 'or 50 iterations' README.md
CMD
expect_fail "negative R6 (removed part): 'iteration limit (50)' occurs nowhere in internal/agent" <<'CMD'
no_match -n -F -e 'iteration limit (50)' internal/agent/*.go
CMD
expect_ok "guard: approval.go, go.mod, go.sum, versions/v1, versions/v5, prompts, .claude unchanged since 4f99ac0" <<'CMD'
git diff --quiet 4f99ac0 -- internal/agent/approval.go go.mod go.sum versions/v1 versions/v5 prompts .claude
CMD
expect_ok "guard: under scripts/verify only v5/T401.sh, v5/T799.sh and v7/ differ from 4f99ac0" <<'CMD'
changed="$( { git diff --name-only 4f99ac0 -- scripts/verify ':!scripts/verify/v7'; git ls-files -o --exclude-standard -- scripts/verify ':!scripts/verify/v7'; } | sort -u)"
printf '%s\n' "$changed"
test -z "$(printf '%s\n' "$changed" | grep -v -x -e '' -e 'scripts/verify/v5/T401.sh' -e 'scripts/verify/v5/T799.sh')"
CMD
expect_ok "guard: Delegation workflow section of CLAUDE.md unchanged since 4f99ac0" <<'CMD'
sec() { awk '/^## /{p = ($0 ~ /^## Delegation workflow/)} p'; }
diff <(git show 4f99ac0:CLAUDE.md | sec) <(sec < CLAUDE.md)
CMD
expect_ok "AC: make check" <<'CMD'
make check
CMD
finish
