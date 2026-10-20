#!/usr/bin/env bash
# Verification for T000 (supervisor-owned; workers never edit it). Exit 0 = pass.
. "$(dirname "$0")/../_lib.sh"
# Phase 0 preflight of versions/v1/plan.md, run by the supervisor itself.
expect_ok "Go 1.27 toolchain" <<'CMD'
go version | grep -q 'go1.27'
CMD
expect_ok "GNU Make" <<'CMD'
make --version
CMD
expect_ok "Git" <<'CMD'
git --version
CMD
expect_ok "gofmt on PATH" <<'CMD'
command -v gofmt
CMD
expect_ok "GOPATH/bin on PATH" <<'CMD'
echo "$PATH" | tr ':' '\n' | grep -qx "$(go env GOPATH)/bin"
CMD
expect_ok "repository root" <<'CMD'
test "$(git rev-parse --show-toplevel)" = /home/sa/workspace/code/yipyip
CMD
expect_ok "clean tree apart from loop-managed paths" <<'CMD'
test -z "$(git status --porcelain | grep -vE ' (Plan\.md|versions/|scripts/verify/)')"
CMD
expect_ok "module proxy reachable for the pinned bubbles version" <<'CMD'
d="$(mktemp -d)" && cd "$d" && go mod download -json github.com/charmbracelet/bubbles@v0.21.1 >/dev/null
CMD
finish
