#!/usr/bin/env bash
# Verification for T503 (supervisor-owned; workers never edit it). Exit 0 = pass.
. "$(dirname "$0")/../_lib.sh"
. "$(dirname "$0")/_probe.sh"
expect_ok "CLAUDE.md mentions the new pieces" <<'CMD'
grep -q -e 'internal/crashlog' CLAUDE.md && grep -q -e 'Prune' CLAUDE.md && grep -q -e 'EvCheckpoint' CLAUDE.md && grep -q -e 'crash.log' CLAUDE.md
CMD
expect_ok "Delegation workflow section is byte-identical to HEAD" <<'CMD'
sec() { sed -n "/^## Delegation workflow/,\$p" "$1"; }; sec CLAUDE.md > "$VTMP/new.txt"; git show HEAD:CLAUDE.md > "$VTMP/old_full.md"; sec "$VTMP/old_full.md" > "$VTMP/old.txt"; cmp "$VTMP/new.txt" "$VTMP/old.txt"
CMD
expect_fail "negative: the Delegation workflow section changed" <<'CMD'
sec() { sed -n "/^## Delegation workflow/,\$p" "$1"; }; sec CLAUDE.md > "$VTMP/n2.txt"; git show HEAD:CLAUDE.md > "$VTMP/o2.md"; sec "$VTMP/o2.md" > "$VTMP/o2.txt"; ! cmp -s "$VTMP/n2.txt" "$VTMP/o2.txt"
CMD
expect_ok "only CLAUDE.md changed" <<'CMD'
test -z "$(git status --porcelain | grep -v -e ' CLAUDE.md' -e ' versions/' -e ' scripts/verify/')"
CMD
expect_ok "make check" <<'CMD'
make check
CMD
finish
