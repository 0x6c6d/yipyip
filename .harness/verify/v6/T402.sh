#!/usr/bin/env bash
# Verification for T402 (supervisor-owned; workers never edit it). Exit 0 = pass.
# The seven README lines the supervisor may edit are selected by these patterns.
. "$(dirname "$0")/../_lib.sh"
expect_ok "baseline commit ede3886 exists" <<'CMD'
git rev-parse -q --verify 'ede3886^{commit}' >/dev/null
CMD
expect_ok "each named line occurs exactly once in the old and the new README" <<'CMD'
for p in '^yipyip is a terminal UI coding assistant' '^- R3\. ' '^- R4\. ' '^- R7\. ' '^- Network \(high-risk, security\):' '^- Security \(high-risk\):' '^- \[ \] A test shows the HTTP transport'; do
	o="$(git show ede3886:README.md | grep -c -E -e "$p")"; n="$(grep -c -E -e "$p" README.md)"
	[ "$o" = 1 ] && [ "$n" = 1 ] || { echo "pattern $p: old $o, new $n"; exit 1; }
done
CMD
expect_ok "every named line was changed" <<'CMD'
for p in '^yipyip is a terminal UI coding assistant' '^- R3\. ' '^- R4\. ' '^- R7\. ' '^- Network \(high-risk, security\):' '^- Security \(high-risk\):' '^- \[ \] A test shows the HTTP transport'; do
	[ "$(git show ede3886:README.md | grep -E -e "$p")" != "$(grep -E -e "$p" README.md)" ] || { echo "unchanged: $p"; exit 1; }
done
CMD
expect_ok "all lines outside the seven named lines unchanged (sections 1-9 and Usage)" <<'CMD'
f() { grep -v -E -e '^yipyip is a terminal UI coding assistant' -e '^- R3\. ' -e '^- R4\. ' -e '^- R7\. ' -e '^- Network \(high-risk, security\):' -e '^- Security \(high-risk\):' -e '^- \[ \] A test shows the HTTP transport'; }
diff <(git show ede3886:README.md | f) <(f < README.md) && test "$(git show ede3886:README.md | wc -l)" = "$(wc -l < README.md)"
CMD
expect_ok "§1 overview names web_search and the read/write rule" <<'CMD'
l="$(grep -E -e '^yipyip is a terminal UI coding assistant' README.md)"
printf '%s' "$l" | grep -q -e 'web_search' && printf '%s' "$l" | grep -q -i -e 'search' && printf '%s' "$l" | grep -q -i -e 'anywhere'
CMD
expect_ok "R3: provider hosts plus search.baseURL for the search client only, web_search, no web fetch" <<'CMD'
l="$(grep -E -e '^- R3\. ' README.md)"
printf '%s' "$l" | grep -q -e 'web_search' && printf '%s' "$l" | grep -q -e 'search.baseURL' && printf '%s' "$l" | grep -q -i -e 'no web fetch' && printf '%s' "$l" | grep -q -e 'bash'
CMD
expect_ok "R4: reads anywhere, write_file/edit_file confined" <<'CMD'
l="$(grep -E -e '^- R4\. ' README.md)"
printf '%s' "$l" | grep -q -e 'read_file' && printf '%s' "$l" | grep -q -e 'write_file' && printf '%s' "$l" | grep -q -e 'edit_file' && printf '%s' "$l" | grep -q -i -e 'anywhere' && printf '%s' "$l" | grep -q -e 'outside the start directory'
CMD
expect_ok "R7: web_search gated with bash, write_file, edit_file, spawn_agent" <<'CMD'
l="$(grep -E -e '^- R7\. ' README.md)"
for w in web_search bash write_file edit_file spawn_agent 'y / n / a'; do printf '%s' "$l" | grep -q -F -e "$w" || { echo "R7 lacks $w"; exit 1; }; done
CMD
expect_ok "§5 Network and Security lines, §8 R3 criterion" <<'CMD'
grep -E -e '^- Network \(high-risk, security\):' README.md | grep -q -e 'search' &&
grep -E -e '^- Network \(high-risk, security\):' README.md | grep -q -e 'BRAVE_API_KEY' &&
grep -E -e '^- Security \(high-risk\):' README.md | grep -q -e 'read_file' &&
grep -E -e '^- Security \(high-risk\):' README.md | grep -q -e 'write_file' &&
grep -E -e '^- \[ \] A test shows the HTTP transport' README.md | grep -q -e 'web_search' &&
grep -E -e '^- \[ \] A test shows the HTTP transport' README.md | grep -q -E -e '\[R3\]$'
CMD
expect_ok "plan phase 4 exit criterion" <<'CMD'
grep -q BRAVE_API_KEY docs/usage.md && grep -q web_search docs/usage.md && grep '^- R7\.' README.md | grep -q web_search && grep '^- R4\.' README.md | grep -q read_file
CMD
expect_fail "negative: old 'It has no web tools' or old R4 all-tools rejection sentence still present" <<'CMD'
no_match -n -F -e 'It has no web tools' -e 'Paths are resolved against the start directory, symlinks are resolved, and any path that ends up outside the start directory is rejected' -e 'The agent has no web fetch or search tool' README.md
CMD
expect_fail "negative: README.md identical to ede3886 (not edited yet)" <<'CMD'
git diff --quiet ede3886 -- README.md
CMD
finish
