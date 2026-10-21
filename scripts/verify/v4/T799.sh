#!/usr/bin/env bash
# Verification for T799 (supervisor-owned; workers never edit it). Exit 0 = pass.
. "$(dirname "$0")/../_lib.sh"
expect_ok "R41-R44, R36, R38, R39: tui and cmd tests by name" <<'CMD'
go_tests ./internal/tui TestInputClick TestInputSelect TestInputSelectSecretNeverCopied TestTabButtons TestTabBarBounds TestTabKeys TestNewline TestHelp TestQuestionInline TestQuestionInlineSecret && go_tests ./cmd/yip TestHelpFlagNoShiftEnter
CMD
expect_ok "R45, R46: skills tests by name" <<'CMD'
go_tests ./internal/skills TestSkillGlobalOnly TestSkillProjectOnly TestSkillProjectOverrides TestSkillBadSkipped && go_tests ./internal/tui TestSkillArgsAppended TestSkillBuiltinWins TestSkillSecretRedacted TestSkillsCommand TestSkillSuggest
CMD
expect_ok "R47, R3, R50: jev client, transport and config tests by name" <<'CMD'
go_tests ./internal/provider TestJevRequestShape TestJevAnswerTypes TestJev422 TestJevForeignHostRefused TestJevHostRefusedUnset && go_tests ./internal/config TestJevRouteUnknownModel
CMD
expect_ok "R48-R52: jev decision tests by name" <<'CMD'
go_tests ./internal/tui TestJevToggle TestJevUnconfigured TestJevStats && go_tests ./internal/agent TestJevQuestion TestJevRoute TestJevRisk TestJevRiskManualUnchanged TestJevRiskSafeUnchanged TestJevFallback
CMD
expect_ok "e2e: TestJevE2E and TestSkillE2E" <<'CMD'
go_tests ./e2e TestJevE2E TestSkillE2E
CMD
expect_ok "phase 5 and phase 6 exit criteria" <<'CMD'
go test -count=1 ./internal/tui/ -run 'TestInputClick|TestInputSelect|TestTabButtons|TestTabBarBounds|TestTabKeys' && go test -count=1 ./internal/tui/ ./cmd/... -run 'TestNewline|TestHelp|TestQuestionInline'
CMD
expect_ok "at least 10 TestJev tests pass repo-wide" <<'CMD'
n="$(go test -count=1 -v ./... 2>&1 | grep -c -e '--- PASS: TestJev')"
echo "TestJev passes: $n"
test "$n" -ge 10
CMD
expect_ok "secret suite repo-wide" <<'CMD'
go test -count=1 ./... -run Secret
CMD
expect_ok "go.mod and go.sum unchanged" <<'CMD'
git diff --quiet 4fabda9 -- go.mod go.sum
CMD
expect_fail "negative: modifyOtherKeys gone from Go code" <<'CMD'
no_match -rn --include='*.go' -e 'modifyOtherKeys' cmd internal
CMD
expect_fail "negative: protected-path diff against 4fabda9 is empty" <<'CMD'
git rev-parse -q --verify '4fabda9^{commit}' >/dev/null || exit 125
paths="README.md Plan.md prompts .claude scripts/verify/_lib.sh versions/v4/spec.md versions/v4/plan.md internal/provider/transport.go internal/tools internal/config/redact.go"
out="$(git diff --stat 4fabda9 -- $paths && git status --porcelain --untracked-files=all -- $paths)" || exit 125
printf '%s' "$out" | no_match -e .
CMD
expect_fail "negative: Delegation workflow section of CLAUDE.md changed" <<'CMD'
! diff <(git show 4fabda9:CLAUDE.md | sed -n '/^## Delegation workflow/,$p') <(sed -n '/^## Delegation workflow/,$p' CLAUDE.md)
CMD
expect_fail "negative: no default HTTP client in non-test code" <<'CMD'
no_match -rn --include='*.go' --exclude='*_test.go' -e 'http\.DefaultClient' -e 'http\.DefaultTransport' -e 'http\.Get(' -e 'http\.Post(' -e 'http\.Head(' cmd internal
CMD
expect_fail "negative: os/exec only in bash.go and clipboard.go" <<'CMD'
grep -rln --include='*.go' --exclude='*_test.go' -e '"os/exec"' cmd internal | grep -v -x -e 'internal/tools/bash.go' -e 'internal/tui/clipboard.go' | no_match -e .
CMD
expect_ok "make check" <<'CMD'
make check
CMD
finish
