#!/usr/bin/env bash
# Verification for T000 (supervisor-owned; workers never edit it). Exit 0 = pass.
. "$(dirname "$0")/../_lib.sh"
expect_ok "working tree is clean" <<'CMD'
s="$(git status --porcelain)"
printf '%s\n' "$s"
test -z "$s"
CMD
expect_ok "v8 start commit a99a82f exists" <<'CMD'
git rev-parse -q --verify 'a99a82f^{commit}'
CMD
expect_ok "code, docs and README identical to a99a82f" <<'CMD'
git diff --quiet a99a82f -- cmd internal e2e go.mod go.sum Makefile config.example.json docs README.md
CMD
expect_ok "go toolchain is go1.27" <<'CMD'
go version | grep -q -e ' go1\.27'
CMD
expect_ok "supervisor probes exist" <<'CMD'
ls scripts/verify/v8/probes/fetch.go scripts/verify/v8/probes/webfetch.go scripts/verify/v8/probes/agentfetch.go
CMD
expect_ok "every v8 task file and executable verify script exists" <<'CMD'
for id in T000 T101 T102 T103 T104 T199 T201 T202 T203 T301 T401 T402 T403 T404 T501 T502 T503 T504 T601 T602 T799; do
	test -x "scripts/verify/v8/$id.sh" && test -f "versions/v8/tasks/$id.md" || { echo "missing: $id"; exit 1; }
done
CMD
expect_ok "Delegation workflow section of CLAUDE.md equals 5a884db" <<'CMD'
diff <(git show 5a884db:CLAUDE.md | sed -n '/^## Delegation workflow/,$p') <(sed -n '/^## Delegation workflow/,$p' CLAUDE.md)
CMD
expect_fail "negative: no web_fetch tool and no NewFetchHTTPClient yet" <<'CMD'
no_match -rn --include='*.go' --exclude='*_test.go' -e 'web_fetch' -e 'NewFetchHTTPClient' cmd internal e2e
CMD
expect_ok "make check" <<'CMD'
make check
CMD
finish
