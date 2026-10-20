#!/usr/bin/env bash
# Verification for T799 (supervisor-owned; workers never edit it). Exit 0 = pass.
. "$(dirname "$0")/../_lib.sh"
# Global Definition of Done of versions/v1/plan.md (D1-D15), run by the supervisor.
expect_ok "D1 go test ./internal/config" <<'CMD'
go test -count=1 ./internal/config
CMD
expect_ok "D2 go test ./internal/provider" <<'CMD'
go test -count=1 ./internal/provider
CMD
expect_ok "D3 go test ./internal/tools" <<'CMD'
go test -count=1 ./internal/tools
CMD
expect_ok "D4 go test ./internal/agent" <<'CMD'
go test -count=1 ./internal/agent
CMD
expect_ok "D5 go test ./internal/tui" <<'CMD'
go test -count=1 ./internal/tui
CMD
expect_ok "D6 go test ./internal/tui -run Status" <<'CMD'
go test -count=1 ./internal/tui -run Status
CMD
expect_ok "D7 go test ./e2e -run Init" <<'CMD'
go test -count=1 ./e2e -run Init
CMD
expect_ok "D8 go test ./e2e -run Compact" <<'CMD'
go test -count=1 ./e2e -run Compact
CMD
expect_ok "D9 go test ./e2e -run Undo" <<'CMD'
go test -count=1 ./e2e -run Undo
CMD
expect_ok "D10 go test ./... -run Secret" <<'CMD'
go test -count=1 ./... -run Secret
CMD
expect_ok "D11 make install; yip --version from / without key" <<'CMD'
env -u OPENROUTER_API_KEY sh -c 'make install && cd / && command -v yip && yip --version' | tail -1 | grep -q '^yip '
CMD
expect_ok "D12 yip --help from an empty temp dir lists the commands" <<'CMD'
sh -c 'o="$(cd "$(mktemp -d)" && yip --help)" || exit 1; for c in /model /effort /init /new /exit /compact /undo /mode /agents /help; do printf %s "$o" | grep -qF -- "$c" || exit 1; done'
CMD
expect_ok "D13 no real provider host in non-test Go code or the Makefile" <<'CMD'
! grep -rn 'openrouter.ai' --include='*.go' . | grep -v '_test.go:' && ! grep -rn 'proxy.golang.org\|openrouter' Makefile
CMD
expect_fail "D13 negative form: grep for the host in non-test Go files finds nothing" <<'CMD'
no_match -rn 'openrouter.ai' --include='*.go' --exclude='*_test.go' .
CMD
expect_ok "D14 example config, appended ## Usage, no deleted README lines" <<'CMD'
B="$(git log --diff-filter=A --format=%H -- versions/v1/plan.md | tail -n 1)"; [ -n "$B" ] || exit 1
test -f config.example.json && grep -q '^## Usage' README.md && git diff --numstat README.md | awk '$2!=0{exit 1}' && git diff --numstat "$B" -- README.md | awk '$2!=0{exit 1}'
CMD
expect_ok "D15 make check" <<'CMD'
make check
CMD
finish
