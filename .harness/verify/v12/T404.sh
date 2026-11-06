#!/usr/bin/env bash
# Verification for T404 (supervisor-owned; workers never edit it). Exit 0 = pass.
. "$(dirname "$0")/../_lib.sh"
expect_ok "CLAUDE.md names the v12 pieces" <<'CMD'
for w in KillAll InitSubreaper 'Shutdown()' SignalQuitMsg reaper Cleanup SIGHUP exitCleanup; do
  grep -qF "$w" CLAUDE.md || { echo "missing: $w"; exit 1; }
done
CMD
expect_ok "Delegation workflow heading still present" <<'CMD'
grep -q "^## Delegation workflow: supervise, don't implement" CLAUDE.md
CMD
expect_ok "security-critical sentence lists the reaper" <<'CMD'
grep -E '^Security-critical logic lives only in' CLAUDE.md | grep -q 'reaper'
CMD
expect_fail "negative: the Delegation workflow section changed" <<'CMD'
sec() { sed -n '/^## Delegation workflow/,/^## /p' | sed '1!{/^## /d}'; }
! diff <(git show 04c57f6:CLAUDE.md | sec) <(sec < CLAUDE.md)
CMD
expect_ok "make check" <<'CMD'
make check
CMD
finish
