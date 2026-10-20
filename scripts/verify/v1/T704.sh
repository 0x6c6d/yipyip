#!/usr/bin/env bash
# Verification for T704 (supervisor-owned; workers never edit it). Exit 0 = pass.
. "$(dirname "$0")/../_lib.sh"
# Baseline: the commit that added versions/v1/plan.md (README.md and CLAUDE.md as specified).
expect_ok "docs/usage.md covers every command and {env:}" <<'CMD'
for c in /model /effort /mode /agents /init /compact /undo /new /clear /exit /quit /help Shift+Tab Esc; do
	grep -qF -- "$c" docs/usage.md || { echo "docs/usage.md lacks $c"; exit 1; }
done
grep -qF '{env:' docs/usage.md
CMD
expect_ok "README.md begins with the original spec, byte for byte" <<'CMD'
B="$(git log --diff-filter=A --format=%H -- versions/v1/plan.md | tail -n 1)"; [ -n "$B" ] || exit 1
git show "$B:README.md" > "$VTMP/readme.orig" || exit 1
n="$(wc -c < "$VTMP/readme.orig")"
head -c "$n" README.md | cmp -s - "$VTMP/readme.orig"
CMD
expect_ok "README.md ends with exactly one appended ## Usage section (D14)" <<'CMD'
B="$(git log --diff-filter=A --format=%H -- versions/v1/plan.md | tail -n 1)"; [ -n "$B" ] || exit 1
n="$(git show "$B:README.md" | wc -c)"
[ "$(grep -c '^## Usage' README.md)" -eq 1 ] || exit 1
tail -c +"$((n + 1))" README.md | grep -q '^## Usage' || exit 1
test -f config.example.json && git diff --numstat "$B" -- README.md | awk '$2!=0{exit 1}'
CMD
expect_ok "CLAUDE.md Delegation workflow section unchanged" <<'CMD'
B="$(git log --diff-filter=A --format=%H -- versions/v1/plan.md | tail -n 1)"; [ -n "$B" ] || exit 1
git show "$B:CLAUDE.md" | sed -n '/^## Delegation workflow/,$p' > "$VTMP/deleg.orig"
[ -s "$VTMP/deleg.orig" ] || exit 1
sed -n '/^## Delegation workflow/,$p' CLAUDE.md | cmp -s - "$VTMP/deleg.orig"
CMD
expect_ok "make check" <<'CMD'
make check
CMD
expect_fail "negative: README.md has no deleted or changed line against the baseline" <<'CMD'
B="$(git log --diff-filter=A --format=%H -- versions/v1/plan.md | tail -n 1)"; [ -n "$B" ] || exit 125
git diff --numstat "$B" -- README.md | awk '$2!=0{bad=1} END{exit bad?0:1}'
CMD
expect_fail "negative: no protected path other than README.md/CLAUDE.md changed" <<'CMD'
git status --porcelain -- prompts .claude Plan.md versions scripts/verify | no_match .
CMD
finish
