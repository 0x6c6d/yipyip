#!/usr/bin/env bash
# Verification for T602 (supervisor-owned; workers never edit it). Exit 0 = pass.
. "$(dirname "$0")/../_lib.sh"
expect_ok "named inline question tests pass" <<'CMD'
go_tests ./internal/tui TestQuestionInline TestQuestionInlineScrolls TestQuestionInlineSecret TestQuestionRedactsSecretInPrompt TestQuestionEnterReturnsChosenLabel TestQuestionEscCancelled
CMD
expect_ok "all question tests of tui and e2e green" <<'CMD'
go test -count=1 -run 'Question' ./internal/tui ./e2e
CMD
expect_ok "write supervisor probe (overlay test, nothing written to the repo)" <<'CMD'
cat > "$VTMP/probe_test.go" <<'EOF'
package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func zzRunes(m Model, s string) Model {
	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)})
	return next.(Model)
}

func zzLines(m Model) string { return strings.Join(m.lines, "\n") }

func TestZZPosInlineChoice(t *testing.T) {
	m, srv, sess := askQuestion(t, "Pick SECRET123?", []string{"a", "b"}, false)
	if !strings.Contains(zzLines(m), questionFreeRow) || !strings.Contains(zzLines(m), "Pick [redacted]?") {
		t.Fatalf("question box not in the transcript lines:\n%s", zzLines(m))
	}
	m = press(m, tea.KeyDown, tea.KeySpace, tea.KeyEnter)
	if got := questionToolResult(t, srv, sess); got != "b" {
		t.Fatalf("tool result = %q, want b", got)
	}
	m = m.Drain()
	v := plain(m)
	if !strings.Contains(v, "Q: Pick [redacted]? → A: b") || strings.Contains(v, questionFreeRow) {
		t.Fatalf("answer line missing or box still shown:\n%s", v)
	}
}

func TestZZPosInlineFreeText(t *testing.T) {
	m, srv, sess := askQuestion(t, "Name?", []string{"a", "b"}, false)
	m = press(m, tea.KeyDown, tea.KeyDown)
	m = zzRunes(m, "my SECRET123")
	if !strings.Contains(plain(m), "my [redacted]") {
		t.Fatalf("free text not shown redacted:\n%s", plain(m))
	}
	m = press(m, tea.KeyEnter)
	_ = questionToolResult(t, srv, sess)
	m = m.Drain()
	if !strings.Contains(plain(m), "Q: Name? → A: my [redacted]") {
		t.Fatalf("answer line missing:\n%s", plain(m))
	}
}

func TestZZPosNoOverlayDim(t *testing.T) {
	m, _, _ := askQuestion(t, "Which?", []string{"a", "b"}, false)
	v := plain(m)
	ask := strings.Index(v, "ask me")
	box := strings.Index(v, questionFreeRow)
	if ask < 0 || box < 0 || ask > box {
		t.Fatalf("user message not above the inline question:\n%s", v)
	}
}

// TestZZEvilSecretShown passes only when the secret shows up in the view, the lines or the clipboard.
func TestZZEvilSecretShown(t *testing.T) {
	m, _, _ := askQuestion(t, "use SECRET123?", []string{"yes SECRET123", "no"}, false)
	clip := &fakeClip{}
	m.clip = clip.write
	m = press(m, tea.KeyDown, tea.KeyDown)
	m = zzRunes(m, "my SECRET123")
	seen := []string{m.View(), zzLines(m)}
	x, y := findCell(t, m, questionFreeRow)
	m, _ = drag(m, 0, max(y-4, 1), x+40, y)
	m = press(m, tea.KeyEnter)
	m = m.Drain()
	seen = append(seen, m.View(), zzLines(m))
	seen = append(seen, clip.texts...)
	for _, s := range seen {
		if strings.Contains(s, "SECRET123") {
			return
		}
	}
	t.Fatal("REJECTED: the secret never reached view, lines or clipboard")
}
EOF
printf '{"Replace":{"%s":"%s"}}' "$ROOT/internal/tui/zz_verify_probe_test.go" "$VTMP/probe_test.go" > "$VTMP/overlay.json"
CMD
expect_ok "probe: box in the transcript, answer lines, redacted free text, message above the box" <<'CMD'
GOFLAGS="-overlay=$VTMP/overlay.json" go_tests ./internal/tui TestZZPosInlineChoice TestZZPosInlineFreeText TestZZPosNoOverlayDim
CMD
expect_fail "negative: SECRET123 never in view, transcript lines or clipboard" <<'CMD'
go test -count=1 -overlay="$VTMP/overlay.json" -run '^TestZZEvilSecretShown$' ./internal/tui
CMD
expect_fail "negative: the question overlay is gone" <<'CMD'
no_match -rn --include='*.go' -e 'overlayQuestion' internal/tui
CMD
expect_ok "go.mod and go.sum unchanged" <<'CMD'
git diff --quiet 4fabda9 -- go.mod go.sum
CMD
expect_fail "negative: no protected, agent or security-critical path touched" <<'CMD'
out="$(git status --porcelain --untracked-files=all -- README.md Plan.md prompts .claude scripts/verify versions/v4/spec.md versions/v4/plan.md versions/v4/tasks internal/provider internal/tools internal/agent internal/config internal/session cmd internal/tui/clipboard.go)" || exit 125
printf '%s' "$out" | no_match -e .
CMD
expect_ok "make check" <<'CMD'
make check
CMD
finish
