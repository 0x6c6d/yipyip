#!/usr/bin/env bash
# Verification for T601 (supervisor-owned; workers never edit it). Exit 0 = pass.
. "$(dirname "$0")/../_lib.sh"
expect_ok "phase 6 exit criterion: README Usage has a medium tier" <<'CMD'
sed -n '/^## Usage/,$p' README.md | grep -q '"medium"'
CMD
expect_ok "README Usage jev example: three tiers, apiKey via env, tier purposes" <<'CMD'
u="$(sed -n '/^## Usage/,$p' README.md)"
for s in '"jev"' '"route"' '"easy"' '"medium"' '"hard"' '{env:TYPESAFE_API_KEY}' 'questions' 'lookups' 'small local edits' 'few files' 'design' 'refactors' 'deep bugs'; do
	printf '%s\n' "$u" | grep -q -F -e "$s" || { echo "README ## Usage lacks: $s"; exit 1; }
done
CMD
expect_ok "README sections 1-9 unchanged against 0c3a7c5" <<'CMD'
diff <(git show 0c3a7c5:README.md | sed '/^## Usage/,$d') <(sed '/^## Usage/,$d' README.md)
CMD
expect_ok "usage doc covers the v5 topics" <<'CMD'
f=docs/usage.md
for s in '"medium"' 'route' '/jev stats' 'no route configured' 'turns routed easy' 'turns not routed' 'jev fallbacks' 'main tokens not routed' 'avg main tokens per turn' '500' 'hard'; do
	grep -q -F -e "$s" "$f" || { echo "usage.md lacks: $s"; exit 1; }
done
CMD
expect_ok "every in-file anchor link of usage.md resolves to a heading" <<'CMD'
f=docs/usage.md
for a in $(grep -o -e '](#[a-z0-9-]*)' "$f" | sed 's/](#//; s/)//' | sort -u); do
	grep -e '^#' "$f" | sed 's/^#* *//' | tr 'A-Z' 'a-z' | sed 's/[^a-z0-9 -]//g; s/ /-/g' | grep -q -x -e "$a" || { echo "dangling anchor: #$a"; exit 1; }
done
CMD
expect_ok "help describes jev routing; help tests pass" <<'CMD'
grep -q -i -e 'rout' internal/tui/help.go && go_tests ./internal/tui TestHelp TestJevHelp && go test -count=1 -run 'Help' ./internal/tui ./cmd/yip
CMD
expect_ok "CLAUDE.md architecture: jev routing only, limits named" <<'CMD'
grep -q -e 'jev.go' CLAUDE.md && grep -q -i -e 'rout' CLAUDE.md && grep -q -e '500' CLAUDE.md
CMD
expect_fail "negative: docs still claim jev answers questions or screens risky calls" <<'CMD'
no_match -n -i -e 'answered by jev' -e 'jev answers' -e 'jev answered' -e 'question answers' -e 'risky' -e 'risk screen' -e 'counters (answered' docs/usage.md CLAUDE.md internal/tui/help.go
CMD
expect_fail "negative: no real key in the docs examples" <<'CMD'
no_match -n -e 'SECRET123' -e 'sk-[A-Za-z0-9]' -e '"apiKey": *"[A-Za-z0-9]' README.md docs/usage.md CLAUDE.md
CMD
expect_ok "Delegation workflow section of CLAUDE.md unchanged" <<'CMD'
diff <(git show 0c3a7c5:CLAUDE.md | sed -n '/^## Delegation workflow/,$p') <(sed -n '/^## Delegation workflow/,$p' CLAUDE.md)
CMD
expect_ok "go.mod and go.sum unchanged" <<'CMD'
git diff --quiet 0c3a7c5 -- go.mod go.sum
CMD
expect_fail "negative: no protected path or code outside help.go touched" <<'CMD'
out="$(git status --porcelain --untracked-files=all -- Plan.md prompts .claude scripts/verify versions/v5/spec.md versions/v5/plan.md versions/v5/tasks internal/provider internal/tools internal/agent internal/config internal/session internal/skills internal/tui cmd e2e | grep -v -e 'internal/tui/help.go' -e 'internal/tui/help_test.go')"
printf '%s' "$out" | no_match -e .
CMD
expect_ok "make check" <<'CMD'
make check
CMD
finish
