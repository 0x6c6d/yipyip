#!/usr/bin/env bash
# Verification for T502 (supervisor-owned; workers never edit it). Exit 0 = pass.
. "$(dirname "$0")/../_lib.sh"
. "$(dirname "$0")/_probe.sh"
expect_ok "heading and key phrases in docs/usage.md" <<'CMD'
grep -q -e 'Crashes, autosave and session retention' docs/usage.md && grep -q -e 'crash.log' docs/usage.md && grep -q -e 'exit code 2' docs/usage.md && grep -q -e '25' docs/usage.md && grep -q -e '--resume' docs/usage.md
CMD
expect_ok "README changed only after the ## Usage heading" <<'CMD'
n=$(grep -n -e '^## Usage' README.md | head -1 | cut -d: -f1); git diff -U0 HEAD -- README.md | grep -e '^@@' | sed -e 's/.*+\([0-9]*\).*/\1/' | while read l; do [ "$l" -gt "$n" ] || { echo "README line $l is above Usage ($n)"; exit 1; }; done
CMD
expect_ok "only docs/usage.md and README.md changed" <<'CMD'
test -z "$(git status --porcelain | grep -v -e ' docs/usage.md' -e ' README.md' -e ' versions/' -e ' scripts/verify/')"
CMD
expect_ok "make check" <<'CMD'
make check
CMD
finish
