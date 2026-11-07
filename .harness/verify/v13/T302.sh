#!/usr/bin/env bash
# Verification for T302 (supervisor-owned; workers never edit it). Exit 0 = pass.
. "$(dirname "$0")/../_lib.sh"
expect_ok "README exists with the core headings" <<'CMD'
f=README.md
test -f "$f" && head -n 1 "$f" | grep -q -e '^# ' \
  && grep -q -x -e '## Features' "$f" && grep -q -x -e '## Requirements' "$f" \
  && grep -q -x -e '## Installation' "$f" && grep -q -x -e '## Quick start' "$f" \
  && grep -q -x -e '## Configuration' "$f" && grep -q -x -e '## Project structure' "$f" \
  && grep -q -x -e '## Development' "$f"
CMD
expect_ok "## headings unchanged since the v13 baseline" <<'CMD'
diff <(git show 81ebe7e:README.md | grep -E '^## ') <(grep -E '^## ' README.md)
CMD
expect_ok "documents the global path, order and limit" <<'CMD'
grep -q -F '~/.config/yipyip/AGENTS.md' README.md && grep -q -F '30,000' README.md \
  && grep -F 'AGENTS.md' README.md | grep -q -i -e 'before'
CMD
expect_ok "says /init does not touch the global file" <<'CMD'
grep -F '/init' README.md | grep -q -i -e 'global'
CMD
expect_fail "negative: no harness references or placeholders" <<'CMD'
no_match -n -F -e '.harness/' -e 'TODO' -e 'TBD' -e '<...>' README.md
CMD
expect_fail "negative: README does not claim yip writes the global AGENTS.md" <<'CMD'
awk '/^### Files yipyip writes/{f=1;next} /^#/{f=0} f' README.md > "$VTMP/writes.txt"
no_match -n -F 'AGENTS.md' "$VTMP/writes.txt"
CMD
expect_ok "Installation and Quick start commands work in a clean checkout" <<'CMD'
co="$VTMP/co"
git clone -q "$ROOT" "$co" || exit 1
(cd "$ROOT" && git ls-files -z -c -o --exclude-standard | xargs -0 tar --ignore-failed-read -cf - 2>/dev/null) | tar -xf - -C "$co" || exit 1
cd "$co" || exit 1
mkdir -p "$VTMP/bin" "$VTMP/home"
GOBIN="$VTMP/bin" timeout 300 make install >"$VTMP/install.log" 2>&1 || { tail -n 20 "$VTMP/install.log"; exit 1; }
test -x "$VTMP/bin/yip" || exit 1
HOME="$VTMP/home" timeout 20 "$VTMP/bin/yip" --version || exit 1
HOME="$VTMP/home" timeout 20 "$VTMP/bin/yip" --help | grep -q -F -e '--resume'
CMD
finish
