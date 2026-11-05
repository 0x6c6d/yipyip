#!/usr/bin/env bash
# Verification for T602 (supervisor-owned; workers never edit it). Exit 0 = pass.
. "$(dirname "$0")/../_lib.sh"
expect_ok "README exists with the core headings" <<'CMD'
test -f README.md
for h in '## Features' '## Requirements' '## Installation' '## Quick start' '## Headless mode' '## Configuration' '## Project structure' '## Development'; do
  grep -q -x -e "$h" README.md || { echo "missing heading: $h"; exit 1; }
done
CMD
expect_ok "headless section documents flags and exit codes" <<'CMD'
test "$(grep -c -- '--max-model-calls' README.md)" -ge 1
for f in '-p' '--json' '--answer' '--approve' '--dir' '--timeout' '--system-append' '--no-save' '--transcript' 'max_calls' 'files_changed'; do
  grep -q -F -e "$f" README.md || { echo "missing: $f"; exit 1; }
done
sed -n '/^## Headless mode/,/^## /p' README.md | grep -q -e '| *5 *|' || { echo "exit code 5 not in a table"; exit 1; }
CMD
expect_fail "negative: no harness references or placeholder text" <<'CMD'
no_match -n -e '\.harness/' -e 'TODO' -e 'TBD' -e '<\.\.\.>' README.md
CMD
expect_ok "README sections other than the touched ones unchanged" <<'CMD'
for h in '## Requirements' '## Installation' '## Security model' '## Development'; do
  a="$(git show HEAD:README.md | awk -v h="$h" '$0==h{p=1;print;next} /^## /{p=0} p')"
  b="$(awk -v h="$h" '$0==h{p=1;print;next} /^## /{p=0} p' README.md)"
  [ "$a" = "$b" ] || { echo "section changed: $h"; exit 1; }
done
CMD
expect_ok "Installation and Quick start commands work in a clean checkout" <<'CMD'
set -e
git clone -q "$ROOT" "$VTMP/co"
cp "$ROOT/README.md" "$VTMP/co/README.md"
cd "$VTMP/co"
GOBIN="$VTMP/bin" timeout 300 make install >/dev/null
"$VTMP/bin/yip" --version | grep -q -e '^yip '
"$VTMP/bin/yip" --help >"$VTMP/help"
grep -q -e '-p' "$VTMP/help" && grep -q -e '--json' "$VTMP/help" && grep -q -e '--answer' "$VTMP/help"
CMD
expect_fail "negative: installed yip -p with empty stdin is a usage error" <<'CMD'
[ -x "$VTMP/bin/yip" ] || exit 125
mkdir -p "$VTMP/h2" && cd "$VTMP/h2" && printf '' | env -i PATH="$PATH" HOME="$VTMP/h2" "$VTMP/bin/yip" -p -; rc=$?; [ "$rc" -eq 2 ] && exit 1; exit 0
CMD
finish
