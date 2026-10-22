#!/usr/bin/env bash
# Verification for T303 (supervisor-owned; workers never edit it). Exit 0 = pass.
. "$(dirname "$0")/../_lib.sh"
expect_ok "named prompt and undo tests" <<'CMD'
go_tests ./internal/agent TestPromptReadWriteRule TestPromptWebSearchMention TestUndoOutsideNotJournaled TestSystemPromptAgentsMD
CMD
expect_ok "plan phase 3 exit (agent part) and whole agent package" <<'CMD'
go test -count=1 ./internal/agent -run 'Undo|Prompt' && go test -count=1 ./internal/agent
CMD
expect_ok "e2e undo still green" <<'CMD'
go test -count=1 ./e2e -run Undo
CMD
expect_ok "write supervisor probe (overlay test, nothing written to the repo)" <<'CMD'
cat > "$VTMP/probe_test.go" <<'EOF'
package agent_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"yipyip/internal/agent"
	"yipyip/internal/config"
	"yipyip/internal/provider/providertest"
)

func zzPromptSession(t *testing.T, withSearch bool) *agent.Session {
	t.Helper()
	srv := providertest.New()
	t.Cleanup(srv.Close)
	home, dir := t.TempDir(), t.TempDir()
	if err := os.MkdirAll(filepath.Dir(config.GlobalPath(home)), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(config.GlobalPath(home), []byte(providertest.ConfigJSON(srv.URL, "k", 8000)), 0o644); err != nil {
		t.Fatal(err)
	}
	if withSearch {
		if err := os.WriteFile(filepath.Join(dir, config.ProjectFile), []byte(`{"search":{"baseURL":"http://127.0.0.1:9","apiKey":"BRAVEKEY-P"}}`), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	cfg, err := config.Load(home, dir, func(string) string { return "" })
	if err != nil {
		t.Fatal(err)
	}
	s, err := agent.NewSession(agent.Options{Config: cfg, StartDir: dir})
	if err != nil {
		t.Fatal(err)
	}
	return s
}

const zzRule = "read_file, glob and grep may read files anywhere on the file system; write_file and edit_file may only change files inside the start directory."

func TestZZPromptPos(t *testing.T) {
	with, without := zzPromptSession(t, true), zzPromptSession(t, false)
	for i := 0; i < 2; i++ {
		pw, po := with.SystemPrompt(), without.SystemPrompt()
		if !strings.Contains(pw, zzRule) || !strings.Contains(po, zzRule) {
			t.Fatalf("read/write rule missing")
		}
		if !strings.Contains(pw, "web_search") || !strings.Contains(po, "You have no web access.") {
			t.Fatalf("web line wrong:\nwith: %q\nwithout: %q", pw, po)
		}
		with.Reset()
		without.Reset()
	}
}

// TestZZPromptEvil passes (exit 0) only when the prompt keeps the old
// confinement sentence, names web_search without search, claims no web
// access with search, or leaks the Brave key.
func TestZZPromptEvil(t *testing.T) {
	pw, po := zzPromptSession(t, true).SystemPrompt(), zzPromptSession(t, false).SystemPrompt()
	switch {
	case strings.Contains(pw+po, "cannot access files outside"):
		t.Log("old confinement sentence kept")
	case strings.Contains(po, "web_search"):
		t.Log("web_search named without search config")
	case strings.Contains(pw, "You have no web access"):
		t.Log("no-web claim with web_search registered")
	case strings.Contains(pw, "BRAVEKEY-P") || strings.Contains(pw, "127.0.0.1:9"):
		t.Log("search key or URL in the prompt")
	default:
		t.Fatal("REJECTED: prompt states the v6 rules")
	}
}
EOF
printf '{"Replace":{"%s":"%s"}}' "$ROOT/internal/agent/zz_verify_probe_test.go" "$VTMP/probe_test.go" > "$VTMP/overlay.json"
CMD
expect_ok "probe: read/write rule in both prompts; web line depends on search; holds after Reset" <<'CMD'
GOFLAGS="-overlay=$VTMP/overlay.json" go_tests ./internal/agent TestZZPromptPos
CMD
expect_fail "negative: old confinement sentence, web_search without search, no-web claim with search, or key/URL in prompt" <<'CMD'
out="$(go test -count=1 -overlay="$VTMP/overlay.json" -run '^TestZZPromptEvil$' ./internal/agent 2>&1)"
printf '%s\n' "$out"
printf '%s\n' "$out" | grep -q -e 'REJECTED:' && exit 1
printf '%s\n' "$out" | grep -q -e '^ok' && exit 0
exit 125
CMD
expect_ok "beforeWrite filters through undoTarget" <<'CMD'
sed -n '/^func (s \*Session) beforeWrite/,/^}/p' internal/agent/undo.go | grep -q -e 'undoTarget('
CMD
expect_fail "negative: a security-critical file differs from HEAD (diff)" <<'CMD'
out="$(git diff --name-only HEAD -- internal/provider/transport.go internal/tools/pathguard.go internal/agent/approval.go internal/tools/bash.go internal/config/redact.go)" || exit 125
printf '%s' "$out" | no_match -e .
CMD
expect_fail "negative: a file outside prompt.go, session.go, undo.go and agent tests changed (or a protected path)" <<'CMD'
out="$(git status --porcelain --untracked-files=all)" || exit 125
printf '%s\n' "$out" | sed -e 's/^...//' -e 's/^.* -> //' | grep -v -x -e '' -e 'versions/v6/Tasks.md' | grep -v -E -x -e 'internal/agent/(prompt|session|undo)\.go' -e 'internal/agent/[a-z0-9_]+_test\.go' | no_match -e .
CMD
expect_ok "go.mod and go.sum unchanged" <<'CMD'
git diff --quiet ede3886 -- go.mod go.sum
CMD
expect_ok "make check" <<'CMD'
make check
CMD
finish
