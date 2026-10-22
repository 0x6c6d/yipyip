#!/usr/bin/env bash
# Verification for T799 (supervisor-owned; workers never edit it). Exit 0 = pass.
. "$(dirname "$0")/../_lib.sh"
expect_ok "R57: go test ./internal/config -run Search; named tests" <<'CMD'
go test -count=1 ./internal/config -run Search && go_tests ./internal/config TestSearchConfigLoad TestSearchConfigDefaults TestSearchConfigAbsent TestSearchConfigMissingKey TestSearchConfigNonHTTPS TestSearchConfigNoEcho TestSearchNotInAllowedBaseURLs TestSearchConfigProject
CMD
expect_ok "R58, R3: go test ./internal/provider -run Search; named tests" <<'CMD'
go test -count=1 ./internal/provider -run Search && go_tests ./internal/provider TestSearchTransportReachesSearchHost TestSearchTransportRefusesProviderHost TestSearchHostRefusedForProviderClient TestSearchHostRefusedForJevClient TestSearchRedirectRefused TestSearchTransportInvalidBaseURL TestSearchClientRequest TestSearchClientResults TestSearchClientNon200 TestSearchClientBadJSON TestSearchClientBodyLimit TestSearchClientCancel
CMD
expect_ok "R59: go test ./internal/tools -run WebSearch; named tests" <<'CMD'
go test -count=1 ./internal/tools -run WebSearch && go_tests ./internal/tools TestWebSearchRequest TestWebSearchCountBounds TestWebSearchQueryBounds TestWebSearchFormat TestWebSearchTruncate TestWebSearchNon200 TestWebSearchBadJSON TestWebSearchNoResults TestWebSearchKeyAbsent TestWebSearchAbsentWithoutSearch TestWebSearchNameRule TestWebSearchSecretRedacted
CMD
expect_ok "R60, R7: go test ./internal/agent -run WebSearch; named tests" <<'CMD'
go test -count=1 ./internal/agent -run WebSearch && go_tests ./internal/agent TestWebSearchIsGated TestWebSearchGateManualPrompt TestWebSearchGateDeny TestWebSearchGateAlways TestWebSearchGateAuto TestWebSearchGateSubagent TestWebSearchManualPrompt TestWebSearchDenySendsNothing TestWebSearchAlwaysRemembered TestWebSearchAutoRuns TestWebSearchSubagentGated TestWebSearchAbsentWithoutConfig
CMD
expect_ok "R61: go test ./internal/agent ./e2e -run Secret; named tests; repo-wide Secret suite" <<'CMD'
go test -count=1 ./internal/agent ./e2e -run Secret && go_tests ./internal/agent TestSecretWebSearch && go_tests ./e2e TestSecretWebSearchE2E TestSecretBraveKeyNotInBashEnv && go test -count=1 ./... -run Secret
CMD
expect_ok "R62, R63, R4: go test ./internal/tools -run Read and -run Write; named tests" <<'CMD'
go test -count=1 ./internal/tools -run Read && go test -count=1 ./internal/tools -run Write && go_tests ./internal/tools TestReadFileAnywhere TestReadGlobOutside TestReadGrepOutside TestReadEtcStyle TestReadRedactedOutside TestReadDanglingSymlink TestWriteConfined TestGuardReadAbsoluteOutside TestGuardReadDotDot TestGuardReadSymlinkOutside TestGuardReadErrors TestGuardWriteStillConfined && go_tests ./internal/agent TestUndoOutsideNotJournaled TestPromptReadWriteRule TestPromptWebSearchMention
CMD
expect_ok "TUI: go test ./internal/tui -run Help; named tests; usage.md documents search" <<'CMD'
go test -count=1 ./internal/tui -run Help && go_tests ./internal/tui TestHelpWebSearch TestApprovalWebSearchQuery TestApprovalWebSearchSecret && grep -q BRAVE_API_KEY docs/usage.md && grep -q '"search"' docs/usage.md
CMD
expect_ok "plan phase exit criteria 1-4" <<'CMD'
go test -count=1 ./internal/config ./internal/provider ./internal/tools -run 'Search|WebSearch' &&
go test -count=1 ./internal/agent ./e2e -run 'WebSearch|Secret' && go test -count=1 ./internal/tui -run 'Help|Approval' &&
go test -count=1 ./internal/tools -run 'Read|Write|PathGuard' && go test -count=1 ./internal/agent -run 'Undo|Prompt' &&
grep -q BRAVE_API_KEY docs/usage.md && grep -q web_search docs/usage.md && grep '^- R7\.' README.md | grep -q web_search && grep '^- R4\.' README.md | grep -q read_file
CMD
expect_ok "README sections 1-9 check (T402)" <<'CMD'
bash scripts/verify/v6/T402.sh
CMD
expect_ok "security files: exactly one commit each since ede3886 for transport.go, pathguard.go, approval.go; none for redact.go, bash.go" <<'CMD'
git rev-parse -q --verify 'ede3886^{commit}' >/dev/null || exit 125
for f in internal/provider/transport.go internal/tools/pathguard.go internal/agent/approval.go; do
	n="$(git log --oneline ede3886..HEAD -- "$f" | wc -l)"; [ "$n" -eq 1 ] || { echo "$f: $n commits"; exit 1; }
done
for f in internal/config/redact.go internal/tools/bash.go; do
	n="$(git log --oneline ede3886..HEAD -- "$f" | wc -l)"; [ "$n" -eq 0 ] || { echo "$f: $n commits"; exit 1; }
done
CMD
expect_fail "negative: uncommitted change to a security-critical file" <<'CMD'
out="$(git status --porcelain -- internal/provider/transport.go internal/tools/pathguard.go internal/agent/approval.go internal/config/redact.go internal/tools/bash.go)" || exit 125
printf '%s' "$out" | no_match -e .
CMD
expect_fail "negative: protected-path diff against ede3886 is not empty" <<'CMD'
git rev-parse -q --verify 'ede3886^{commit}' >/dev/null || exit 125
paths="Plan.md prompts .claude scripts/verify/_lib.sh versions/v6/spec.md versions/v6/plan.md"
out="$(git diff --stat ede3886 -- $paths && git status --porcelain --untracked-files=all -- $paths)" || exit 125
printf '%s' "$out" | no_match -e .
CMD
expect_ok "Delegation workflow section of CLAUDE.md unchanged against ede3886 (architecture notes may change)" <<'CMD'
diff <(git show ede3886:CLAUDE.md | sed -n '/^## Delegation workflow/,$p') <(sed -n '/^## Delegation workflow/,$p' CLAUDE.md)
CMD
expect_fail "negative: default HTTP client in non-test code" <<'CMD'
no_match -rn --include='*.go' --exclude='*_test.go' -e 'http\.DefaultClient' -e 'http\.DefaultTransport' -e 'http\.Get(' -e 'http\.Post(' -e 'http\.Head(' cmd internal
CMD
expect_fail "negative: http.Client or http.Transport built outside transport.go" <<'CMD'
grep -rln --include='*.go' --exclude='*_test.go' -e 'http\.Client{' -e 'http\.Transport{' cmd internal | grep -v -x -e 'internal/provider/transport.go' | no_match -e .
CMD
expect_fail "negative: os/exec outside bash.go and clipboard.go" <<'CMD'
grep -rln --include='*.go' --exclude='*_test.go' -e '"os/exec"' cmd internal | grep -v -x -e 'internal/tools/bash.go' -e 'internal/tui/clipboard.go' | no_match -e .
CMD
expect_fail "negative: Makefile names a real host (Brave, OpenRouter, proxy)" <<'CMD'
no_match -n -i -e 'brave' -e 'openrouter' -e 'proxy.golang.org' Makefile
CMD
expect_ok "go.mod and go.sum unchanged" <<'CMD'
git diff --quiet ede3886 -- go.mod go.sum
CMD
expect_ok "make check" <<'CMD'
make check
CMD
finish
