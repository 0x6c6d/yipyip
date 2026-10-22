#!/usr/bin/env bash
# Verification for T204 (supervisor-owned; workers never edit it). Exit 0 = pass.
. "$(dirname "$0")/../_lib.sh"
expect_ok "named TUI tests" <<'CMD'
go_tests ./internal/tui TestApprovalWebSearchQuery TestApprovalWebSearchSecret TestHelpWebSearch
CMD
expect_ok "TUI Help and Approval tests (plan phase 2 exit)" <<'CMD'
go test -count=1 ./internal/tui -run 'Help|Approval'
CMD
expect_ok "TestHelpCommand still checks every command (only the window size may change)" <<'CMD'
go_tests ./internal/tui TestHelpCommand && sed -n '/^func TestHelpCommand/,/^}/p' internal/tui/commands_test.go | grep -q -e 'range Commands()'
CMD
expect_ok "write supervisor probe (overlay test, nothing written to the repo)" <<'CMD'
cat > "$VTMP/probe_test.go" <<'EOF'
package tui

import (
	"strings"
	"testing"
)

func TestZZTUIWebSearchPos(t *testing.T) {
	if got := argsSummary("web_search", `{"query":"rust borrow checker","count":2}`); got != "rust borrow checker" {
		t.Fatalf("summary %q", got)
	}
	if got := argsSummary("web_search", `{"count":2}`); got != `{"count":2}` {
		t.Fatalf("fallback %q", got)
	}
	if got := argsSummary("bash", `{"command":"ls"}`); got != "ls" {
		t.Fatalf("bash summary changed: %q", got)
	}
	h := HelpText()
	for _, w := range []string{"web_search", `"search"`, "BRAVE_API_KEY", "/help", "Keys:"} {
		if !strings.Contains(h, w) {
			t.Fatalf("help lacks %q", w)
		}
	}
}

// TestZZTUIWebSearchEvil passes (exit 0) only when the help text names cost
// or a price.
func TestZZTUIWebSearchEvil(t *testing.T) {
	h := strings.ToLower(HelpText())
	for _, bad := range []string{"cost", "$", "price"} {
		if strings.Contains(h, bad) {
			t.Logf("help contains %q", bad)
			return
		}
	}
	t.Fatal("REJECTED: help names no cost")
}
EOF
printf '{"Replace":{"%s":"%s"}}' "$ROOT/internal/tui/zz_verify_probe_test.go" "$VTMP/probe_test.go" > "$VTMP/overlay.json"
CMD
expect_ok "probe: web_search summary is the query, fallback raw args, help names web_search, search, BRAVE_API_KEY" <<'CMD'
GOFLAGS="-overlay=$VTMP/overlay.json" go_tests ./internal/tui TestZZTUIWebSearchPos
CMD
expect_fail "negative: help text names cost or a price" <<'CMD'
out="$(go test -count=1 -overlay="$VTMP/overlay.json" -run '^TestZZTUIWebSearchEvil$' ./internal/tui 2>&1)"
printf '%s\n' "$out"
printf '%s\n' "$out" | grep -q -e 'REJECTED:' && exit 1
printf '%s\n' "$out" | grep -q -e '^ok' && exit 0
exit 125
CMD
expect_fail "negative: a security-critical file differs from HEAD (diff)" <<'CMD'
out="$(git diff --name-only HEAD -- internal/provider/transport.go internal/tools/pathguard.go internal/agent/approval.go internal/tools/bash.go internal/config/redact.go)" || exit 125
printf '%s' "$out" | no_match -e .
CMD
expect_fail "negative: a file outside tui approval.go/help.go and TUI tests changed (or a protected path)" <<'CMD'
out="$(git status --porcelain --untracked-files=all)" || exit 125
printf '%s\n' "$out" | sed -e 's/^...//' -e 's/^.* -> //' | grep -v -x -e '' -e 'versions/v6/Tasks.md' | grep -v -E -x -e 'internal/tui/(approval|help)\.go' -e 'internal/tui/[a-z0-9_]+_test\.go' | no_match -e .
CMD
expect_ok "go.mod and go.sum unchanged" <<'CMD'
git diff --quiet ede3886 -- go.mod go.sum
CMD
expect_ok "make check" <<'CMD'
make check
CMD
finish
