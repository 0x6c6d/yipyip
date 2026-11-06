#!/usr/bin/env bash
# Verification for T403 (supervisor-owned; workers never edit it). Exit 0 = pass.
. "$(dirname "$0")/../_lib.sh"
expect_ok "README exists with the core headings" <<'CMD'
test -f README.md
for h in '## Features' '## Requirements' '## Installation' '## Quick start' '## Configuration' '## Project structure' '## Development'; do
  grep -E '^## ' README.md | grep -qxF "$h" || { echo "missing heading: $h"; exit 1; }
done
CMD
expect_ok "README documents the cleanup (background, signals, 2 s grace, Linux subreaper)" <<'CMD'
test "$(grep -ci 'background' README.md)" -ge 1
grep -q 'SIGTERM' README.md && grep -q 'SIGKILL' README.md
grep -qE -e '2 ?s\b' -e '2 seconds' README.md
grep -qi 'subreaper' README.md && grep -q 'Linux' README.md
CMD
expect_fail "negative: no harness references or placeholders" <<'CMD'
grep -n -e '\.harness/' -e 'TODO' -e 'TBD' -e '<\.\.\.>' README.md
CMD
expect_ok "Installation and Quick start commands work in a clean copy" <<'CMD'
set -e
mkdir -p "$VTMP/c" "$VTMP/bin"
git ls-files -z --cached --others --exclude-standard | xargs -0 tar cf - --ignore-failed-read 2>/dev/null | tar xf - -C "$VTMP/c"
cd "$VTMP/c"
timeout 300 env GOBIN="$VTMP/bin" make install
test -x "$VTMP/bin/yip"
timeout 30 "$VTMP/bin/yip" --version
timeout 30 "$VTMP/bin/yip" --help | grep -q 'Usage: yip'
timeout 300 make build
test -x bin/yip
CMD
finish
