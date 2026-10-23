#!/usr/bin/env bash
# Verification for T203 (supervisor-owned; workers never edit it). Exit 0 = pass.
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
# mutant_commit <dir>: commits a mutation made in a snapshot.
mutant_commit() {
	(cd "$1" && git -c user.name=verify -c user.email=verify@localhost -c commit.gpgsign=false -c core.hooksPath=/dev/null commit -q -am 'mutant') || return 125
}
export -f snapshot mutant_commit

expect_ok "T799.sh: bash syntax, executable, sources _lib.sh" <<'CMD'
f=scripts/verify/v5/T799.sh
bash -n "$f" && test -x "$f" && grep -q -F -e '. "$(dirname "$0")/../_lib.sh"' "$f"
CMD
expect_ok "R67: T799.sh runs TestMainNoIterationLimit and TestSubagentIterationLimit" <<'CMD'
grep -q -e 'TestMainNoIterationLimit' scripts/verify/v5/T799.sh && grep -q -e 'TestSubagentIterationLimit' scripts/verify/v5/T799.sh
CMD
expect_ok "R64/R54: T799.sh checks MaxIterations = 0 and MaxSubagentIterations = 500" <<'CMD'
grep -q -F -e 'const MaxIterations = 0' scripts/verify/v5/T799.sh && grep -q -F -e 'const MaxSubagentIterations = 500' scripts/verify/v5/T799.sh
CMD
expect_ok "T799.sh: approval.go guard is rebased to 4f99ac0" <<'CMD'
grep -n -e 'approval\.go' scripts/verify/v5/T799.sh | grep -q -e '4f99ac0'
CMD
expect_fail "negative R67: no main-50 names, assertions or old approval base in T799.sh" <<'CMD'
no_match -n -F -e 'TestMainIterationLimit' -e 'TestZZPosMain50' -e 'TestZZEvil' -e 'MaxIterations != 50' -e 'const MaxIterations = 50' -e 'ba7d939^' scripts/verify/v5/T799.sh
CMD
expect_ok "scope: under scripts/verify only v5/T401.sh, v5/T799.sh and v7/ differ from 4f99ac0" <<'CMD'
changed="$( { git diff --name-only 4f99ac0 -- scripts/verify ':!scripts/verify/v7'; git ls-files -o --exclude-standard -- scripts/verify ':!scripts/verify/v7'; } | sort -u)"
printf '%s\n' "$changed"
test -z "$(printf '%s\n' "$changed" | grep -v -x -e '' -e 'scripts/verify/v5/T401.sh' -e 'scripts/verify/v5/T799.sh')"
CMD
expect_ok "R67: T799.sh exits 0 in a committed snapshot of the working tree" <<'CMD'
snapshot ok || exit 125
cd "$VTMP/ok" && bash scripts/verify/v5/T799.sh
CMD
expect_fail "negative: T799.sh fails when approval.go drifts from 4f99ac0" <<'CMD'
snapshot appr || exit 125
printf '\n// v7 verify: approval drift probe\n' >> "$VTMP/appr/internal/agent/approval.go"
mutant_commit "$VTMP/appr" || exit 125
cd "$VTMP/appr" && bash scripts/verify/v5/T799.sh
CMD
expect_fail "negative: T799.sh fails when README sections 1-9 change outside the R6 lines" <<'CMD'
snapshot readme || exit 125
cd "$VTMP/readme" || exit 125
sed -i 's/^## 1\. Overview$/## 1. Overview (drift probe)/' README.md
grep -q -e '^## 1\. Overview (drift probe)$' README.md || exit 125
mutant_commit "$VTMP/readme" || exit 125
bash scripts/verify/v5/T799.sh
CMD
finish
