#!/usr/bin/env bash
# Verification for T303 (supervisor-owned; workers never edit it). Exit 0 = pass.
. "$(dirname "$0")/../_lib.sh"
expect_ok "CLAUDE.md notes the global AGENTS.md" <<'CMD'
grep -q -F '~/.config/yipyip/AGENTS.md' CLAUDE.md && grep -q -F 'GlobalDir' CLAUDE.md \
  && grep -q -F 'readGlobalAgentsFile' CLAUDE.md && grep -q -F 'GlobalAgents' CLAUDE.md
CMD
expect_ok "Delegation workflow section unchanged" <<'CMD'
sec() { awk '/^## Delegation workflow/{f=1} f' ; }
diff <(git show 81ebe7e:CLAUDE.md | sec) <(sec < CLAUDE.md)
CMD
expect_fail "negative: Delegation workflow section missing or changed" <<'CMD'
sec() { awk '/^## Delegation workflow/{f=1} f' ; }
test -n "$(sec < CLAUDE.md)" && cmp -s <(git show 81ebe7e:CLAUDE.md | sec) <(sec < CLAUDE.md) && exit 1
echo "section differs"; exit 0
CMD
expect_ok "build commands still listed" <<'CMD'
grep -q -F 'make check' CLAUDE.md && grep -q -F 'go test ./...' CLAUDE.md
CMD
finish
