#!/usr/bin/env bash
# Verification for T202 (supervisor-owned; workers never edit it). Exit 0 = pass.
: "${VERIFY_TIMEOUT:=1200}"
. "$(dirname "$0")/../_lib.sh"

# snapshot <name>: clones the repo into $VTMP/<name> and commits the current uncommitted
# changes (modified, untracked, deleted) on top, so scripts that require a committed tree
# can run before the supervisor's commit. Returns 125 on a setup error.
snapshot() {
	local dst="$VTMP/$1"
	git clone -q "$ROOT" "$dst" || return 125
	(cd "$ROOT" && git ls-files -z -m -o --exclude-standard | tar --null --ignore-failed-read -T - -cf - 2>/dev/null) | tar -xf - -C "$dst" || return 125
	(cd "$ROOT" && git ls-files -z -d) | (cd "$dst" && xargs -0 -r rm -f --)
	(cd "$dst" && git add -A && git -c user.name=verify -c user.email=verify@localhost -c commit.gpgsign=false -c core.hooksPath=/dev/null commit -q --allow-empty -m 'verify snapshot') || return 125
}
export -f snapshot

expect_ok "T401.sh: bash syntax, executable, sources _lib.sh" <<'CMD'
f=scripts/verify/v5/T401.sh
bash -n "$f" && test -x "$f" && grep -q -F -e '. "$(dirname "$0")/../_lib.sh"' "$f"
CMD
expect_ok "R67: T401.sh runs TestMainNoIterationLimit and TestSubagentIterationLimit" <<'CMD'
grep -q -e 'TestMainNoIterationLimit' scripts/verify/v5/T401.sh && grep -q -e 'TestSubagentIterationLimit' scripts/verify/v5/T401.sh
CMD
expect_ok "R64: T401.sh has the TestZZPosMainUnlimited probe (121 requests)" <<'CMD'
f=scripts/verify/v5/T401.sh
grep -q -e 'TestZZPosMainUnlimited' "$f" && grep -q -e '121' "$f" && grep -q -F -e 'MaxIterations != 0' "$f"
CMD
expect_ok "R54: T401.sh keeps TestZZPosSubagent500 and 'iteration limit (500) reached'" <<'CMD'
grep -q -e 'TestZZPosSubagent500' scripts/verify/v5/T401.sh && grep -q -F -e 'iteration limit (500) reached' scripts/verify/v5/T401.sh
CMD
expect_ok "R64: T401.sh has the negative 'iteration limit (50)' check on internal/agent" <<'CMD'
grep -n -F -e 'iteration limit (50)' scripts/verify/v5/T401.sh | grep -q -e 'internal/agent'
CMD
expect_fail "negative R67: no main-50 names or assertions left in T401.sh" <<'CMD'
no_match -n -F -e 'TestMainIterationLimit' -e 'TestZZPosMain50' -e 'TestZZEvil' -e 'MaxIterations != 50' scripts/verify/v5/T401.sh
CMD
expect_ok "scope: under scripts/verify only v5/T401.sh, v5/T799.sh and v7/ differ from 4f99ac0" <<'CMD'
changed="$( { git diff --name-only 4f99ac0 -- scripts/verify ':!scripts/verify/v7'; git ls-files -o --exclude-standard -- scripts/verify ':!scripts/verify/v7'; } | sort -u)"
printf '%s\n' "$changed"
test -z "$(printf '%s\n' "$changed" | grep -v -x -e '' -e 'scripts/verify/v5/T401.sh' -e 'scripts/verify/v5/T799.sh')"
CMD
expect_ok "R67: T401.sh exits 0 in a committed snapshot of the working tree" <<'CMD'
snapshot ok || exit 125
cd "$VTMP/ok" && bash scripts/verify/v5/T401.sh
CMD
expect_fail "negative R67: T401.sh fails when the main limit returns (const MaxIterations = 50)" <<'CMD'
snapshot mut || exit 125
cd "$VTMP/mut" || exit 125
sed -i 's/^const MaxIterations = 0$/const MaxIterations = 50/' internal/agent/loop.go
grep -q -e '^const MaxIterations = 50$' internal/agent/loop.go || exit 125
git -c user.name=verify -c user.email=verify@localhost -c core.hooksPath=/dev/null commit -q -am 'mutant' || exit 125
bash scripts/verify/v5/T401.sh
CMD
finish
