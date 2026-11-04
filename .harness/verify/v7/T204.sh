#!/usr/bin/env bash
# Verification for T204 (supervisor-owned; workers never edit it). Exit 0 = pass.
: "${VERIFY_TIMEOUT:=1200}"
. "$(dirname "$0")/../_lib.sh"
expect_ok "committed: no uncommitted change in README.md, .harness/Plan.md, internal/, .harness/verify/" <<'CMD'
s="$(git status --porcelain -- README.md .harness/Plan.md internal .harness/verify)"
printf '%s\n' "$s"
test -z "$s"
CMD
expect_ok "R67: .harness/verify/v5/T401.sh exits 0" <<'CMD'
bash .harness/verify/v5/T401.sh
CMD
expect_ok "R67: .harness/verify/v5/T799.sh exits 0" <<'CMD'
bash .harness/verify/v5/T799.sh
CMD
expect_fail "negative R67: no main-50 names or assertions in .harness/verify/v5" <<'CMD'
no_match -n -F -e 'TestMainIterationLimit' -e 'TestZZPosMain50' -e 'MaxIterations != 50' .harness/verify/v5/*.sh
CMD
expect_fail "negative R6: old R6 phrases are gone from README.md" <<'CMD'
no_match -n -e 'iteration limit stops at 50' -e 'or 50 iterations' README.md
CMD
finish
