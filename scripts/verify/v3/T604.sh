#!/usr/bin/env bash
# Verification for T604 (supervisor-owned; workers never edit it). Exit 0 = pass.
. "$(dirname "$0")/../_lib.sh"
expect_ok "help tests + cmd help test" <<'CMD'
go_tests ./internal/tui TestHelpListsNewCommands TestHelpStillListsOldEntries
go_tests ./cmd/yip TestHelpFlagListsNewFeatures
CMD
expect_ok "docs/usage.md has the four new sections" <<'CMD'
for s in Sessions Tabs Questions Retry; do grep -qi -e '^#* '"$s" docs/usage.md || exit 1; done
CMD
expect_ok "usage docs mention the load-bearing strings" <<'CMD'
grep -q -- '--resume' docs/usage.md && grep -q 'could not save settings' docs/usage.md && grep -q 'tab limit reached' docs/usage.md
CMD
expect_ok "CLAUDE.md mentions internal/session and the newer tui files" <<'CMD'
grep -q 'internal/session' CLAUDE.md && grep -q 'question' CLAUDE.md
CMD
expect_ok "--help and /help carry the new feature strings" <<'CMD'
out="$(go run ./cmd/yip --help)" || exit 1
printf '%s\n' "$out" | grep -q -- '--resume' || exit 1
printf '%s\n' "$out" | grep -qi 'shift+enter' || exit 1
grep -rq '/sessions' internal/tui/help.go
CMD
expect_fail "negative: the Delegation workflow section of CLAUDE.md is untouched" <<'CMD'
out="$(git diff --stat 9519824 -- CLAUDE.md)" || exit 125
if [ -n "$out" ]; then
  git show 9519824:CLAUDE.md | sed -n '/^## Delegation workflow/,/^## /p' > "$VTMP/dw-old.txt"
  sed -n '/^## Delegation workflow/,/^## /p' CLAUDE.md > "$VTMP/dw-new.txt"
  cmp -s "$VTMP/dw-old.txt" "$VTMP/dw-new.txt" || exit 1
fi
CMD
expect_fail "negative: README.md (the spec) untouched" <<'CMD'
out="$(git diff --stat 9519824 -- README.md && git status --porcelain --untracked-files=all -- README.md)" || exit 125
printf '%s' "$out" | no_match -e .
CMD
expect_ok "make check" <<'CMD'
make check
CMD
finish
