#!/usr/bin/env bash
# Verification for T502 (supervisor-owned; workers never edit it). Exit 0 = pass.
. "$(dirname "$0")/../_lib.sh"
expect_ok "named input selection tests (and the click test) pass" <<'CMD'
go_tests ./internal/tui TestInputSelect TestInputSelectHighlight TestInputSelectSecretNeverCopied TestInputClick
CMD
expect_ok "conversation copy-on-select unchanged" <<'CMD'
go_tests ./internal/tui TestCopySelection TestCopyTrailingSpacesTrimmed TestCopyHighlightReverse TestCopyNotice TestCopyClickNoDrag TestCopySecretNeverCopied
CMD
expect_ok "write supervisor probe (overlay test, nothing written to the repo)" <<'CMD'
cat > "$VTMP/probe_test.go" <<'EOF'
package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"yipyip/internal/provider/providertest"
)

func zzInput(t *testing.T, text string) (Model, *fakeClip) {
	t.Helper()
	srv := providertest.New(providertest.Response{Text: "ok"})
	t.Cleanup(srv.Close)
	m, _ := newSessionModel(t, srv)
	m = upd(m, tea.WindowSizeMsg{Width: 100, Height: 30})
	clip := &fakeClip{}
	m.clip = clip.write
	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(text)})
	return next.(Model), clip
}

func TestZZPosInputCopy(t *testing.T) {
	m, clip := zzInput(t, "alpha beta")
	x, y := findCell(t, m, "alpha beta")
	m = upd(m, leftMouse(x+2, y, tea.MouseActionPress))
	m = upd(m, leftMouse(x+2, y, tea.MouseActionRelease))
	if len(clip.texts) != 0 {
		t.Fatalf("a click copied %q", clip.texts)
	}
	m, _ = drag(m, x, y, x+9, y)
	if len(clip.texts) != 1 || clip.texts[0] != "alpha beta" {
		t.Fatalf("drag over the input copied %q, want [\"alpha beta\"]", clip.texts)
	}
	if !strings.Contains(plainView(m), "10 characters copied") {
		t.Fatalf("no notice:\n%s", plainView(m))
	}
	if m.input.Value() != "alpha beta" {
		t.Fatalf("drag changed the input to %q", m.input.Value())
	}
}

func TestZZPosSecretRedacted(t *testing.T) {
	m, clip := zzInput(t, "key SECRET123 end")
	x, y := findCell(t, m, "key ")
	m, _ = drag(m, x, y, x+60, y)
	if !strings.Contains(plainView(m), "characters copied") {
		t.Fatalf("no copy notice:\n%s", plainView(m))
	}
	if len(clip.texts) != 1 || !strings.Contains(clip.texts[0], "[redacted]") || !strings.HasPrefix(clip.texts[0], "key ") {
		t.Fatalf("copied %q, want one redacted text", clip.texts)
	}
}

// TestZZEvilSecretCopied passes only when the secret reached the clipboard.
func TestZZEvilSecretCopied(t *testing.T) {
	m, clip := zzInput(t, "key SECRET123 end")
	x, y := findCell(t, m, "key ")
	m, _ = drag(m, x+4, y, x+60, y) // only the secret and what follows
	_, _ = drag(m, x, y, x+60, y)
	for _, s := range clip.texts {
		if strings.Contains(s, "SECRET123") {
			return
		}
	}
	t.Fatal("REJECTED: the secret never reached the clipboard")
}
EOF
printf '{"Replace":{"%s":"%s"}}' "$ROOT/internal/tui/zz_verify_probe_test.go" "$VTMP/probe_test.go" > "$VTMP/overlay.json"
CMD
expect_ok "probe: drag copies input text with notice, click copies nothing, secret redacted" <<'CMD'
GOFLAGS="-overlay=$VTMP/overlay.json" go_tests ./internal/tui TestZZPosInputCopy TestZZPosSecretRedacted
CMD
expect_fail "negative: SECRET123 never copied from the input" <<'CMD'
go test -count=1 -overlay="$VTMP/overlay.json" -run '^TestZZEvilSecretCopied$' ./internal/tui
CMD
expect_fail "negative: no new exec or clipboard writer in the tui" <<'CMD'
grep -rln --include='*.go' -e '"os/exec"' -e 'atotto/clipboard' internal/tui | grep -v -e 'internal/tui/clipboard.go' | no_match -e .
CMD
expect_fail "negative: the input selection code never bypasses m.clip" <<'CMD'
no_match -n -e 'writeClipboard' -e 'defaultClipboard' -e 'os.Stdout' internal/tui/inputmouse.go internal/tui/mouse.go
CMD
expect_ok "go.mod and go.sum unchanged" <<'CMD'
git diff --quiet 4fabda9 -- go.mod go.sum
CMD
expect_fail "negative: no protected or security-critical path touched" <<'CMD'
out="$(git status --porcelain --untracked-files=all -- README.md Plan.md prompts .claude scripts/verify versions/v4/spec.md versions/v4/plan.md versions/v4/tasks internal/provider internal/tools internal/agent internal/config internal/session cmd internal/tui/clipboard.go)" || exit 125
printf '%s' "$out" | no_match -e .
CMD
expect_ok "make check" <<'CMD'
make check
CMD
finish
