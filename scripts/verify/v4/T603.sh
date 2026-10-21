#!/usr/bin/env bash
# Verification for T603 (supervisor-owned; workers never edit it). Exit 0 = pass.
. "$(dirname "$0")/../_lib.sh"
expect_ok "help tests pass" <<'CMD'
go_tests ./internal/tui TestHelpMouseAndTabs TestHelp TestJevHelp && go test -count=1 -run 'Help' ./internal/tui ./cmd/yip
CMD
expect_ok "phase 6 exit criterion" <<'CMD'
go_tests ./internal/tui TestNewline TestHelp TestQuestionInline && go test -count=1 ./internal/tui/ ./cmd/... -run 'TestNewline|TestHelp|TestQuestionInline' && ! grep -rn 'modifyOtherKeys' --include='*.go' cmd internal
CMD
expect_ok "usage doc covers the v4 topics" <<'CMD'
f=docs/usage.md
for s in '## Skills' '## Jev decisions' 'SKILL.md' '/skills' '/jev' '/jev stats' 'TYPESAFE_API_KEY' '.yipyip/skills' '.config/yipyip/skills' 'route' 'Alt+Enter' 'Ctrl+T' 'Ctrl+W' '×' 'Q: '; do
	grep -q -F -e "$s" "$f" || { echo "usage.md lacks: $s"; exit 1; }
done
CMD
expect_ok "CLAUDE.md architecture names the new code" <<'CMD'
for s in 'internal/skills' 'jev.go' 'Skill' 'Jev'; do
	grep -q -F -e "$s" CLAUDE.md || { echo "CLAUDE.md lacks: $s"; exit 1; }
done
CMD
expect_ok "every internal/tui file named in CLAUDE.md exists" <<'CMD'
line="$(grep -e '^- `internal/tui`' CLAUDE.md)" || exit 1
for f in $(printf '%s\n' "$line" | grep -o -e '`[a-z_]*\.go`' | tr -d '`' | grep -v -x -e 'bash.go' | sort -u); do
	test -f "internal/tui/$f" || { echo "CLAUDE.md names missing internal/tui/$f"; exit 1; }
done
CMD
expect_fail "negative: removed keys are not documented" <<'CMD'
no_match -n -i -e 'shift+enter' -e 'ctrl+tab' -e 'alt+1' -e 'extended-keys' -e 'question overlay' docs/usage.md CLAUDE.md internal/tui/help.go
CMD
expect_fail "negative: no real key in the docs examples" <<'CMD'
no_match -n -e 'SECRET123' -e 'sk-[A-Za-z0-9]' -e '"apiKey": *"[A-Za-z0-9]' docs/usage.md CLAUDE.md
CMD
expect_ok "Delegation workflow section of CLAUDE.md unchanged" <<'CMD'
diff <(git show 4fabda9:CLAUDE.md | sed -n '/^## Delegation workflow/,$p') <(sed -n '/^## Delegation workflow/,$p' CLAUDE.md)
CMD
expect_ok "README.md (spec) unchanged" <<'CMD'
git diff --quiet 4fabda9 -- README.md && test -z "$(git status --porcelain -- README.md)"
CMD
expect_ok "go.mod and go.sum unchanged" <<'CMD'
git diff --quiet 4fabda9 -- go.mod go.sum
CMD
expect_fail "negative: no protected path or production package touched" <<'CMD'
out="$(git status --porcelain --untracked-files=all -- Plan.md prompts .claude scripts/verify versions/v4/spec.md versions/v4/plan.md versions/v4/tasks internal/provider internal/tools internal/agent internal/config internal/session internal/skills cmd e2e)" || exit 125
printf '%s' "$out" | no_match -e .
CMD
expect_ok "make check" <<'CMD'
make check
CMD
finish
