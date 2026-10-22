#!/usr/bin/env bash
# Verification for T501 (supervisor-owned; workers never edit it). Exit 0 = pass.
. "$(dirname "$0")/../_lib.sh"
expect_ok "named click tests and the v4 input mouse tests" <<'CMD'
go_tests ./internal/tui TestInputClickMultiLine TestInputClickWrapped TestInputClickScrolled TestInputClickWideRunes TestInputClickOutside TestInputClick TestInputSelect TestInputSelectHighlight TestInputSelectSecretNeverCopied
CMD
expect_ok "phase 5 exit criterion" <<'CMD'
go test -count=1 ./internal/tui/ -run 'InputClick'
CMD
expect_ok "new tests send a real tea.MouseMsg through Update" <<'CMD'
grep -q -e 'tea.MouseMsg' internal/tui/inputmouse_test.go && grep -q -e 'MouseActionPress' internal/tui/inputmouse_test.go && grep -q -e 'MouseActionRelease' internal/tui/inputmouse_test.go
CMD
expect_ok "write supervisor probe (overlay test, nothing written to the repo)" <<'CMD'
cat > "$VTMP/probe_test.go" <<'EOF'
package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/bubbles/textarea"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
	"github.com/mattn/go-runewidth"
)

// zzUpd is Update followed by View, as the Bubble Tea runtime renders after
// every message.
func zzUpd(m Model, msg tea.Msg) Model {
	next, _ := m.Update(msg)
	m = next.(Model)
	_ = m.View()
	return m
}

// zzModel types value through Update (alt+enter for newlines), like a user.
func zzModel(t *testing.T, w, h int, value string) Model {
	t.Helper()
	m := zzUpd(New(Options{StartDir: "/x", Clipboard: func(string) error { return nil }}), tea.WindowSizeMsg{Width: w, Height: h})
	for _, r := range value {
		if r == '\n' {
			m = zzUpd(m, tea.KeyMsg{Type: tea.KeyEnter, Alt: true})
			continue
		}
		m = zzUpd(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}
	if m.input.Value() != value {
		t.Fatalf("typed %q, input holds %q", value, m.input.Value())
	}
	return m
}

// zzAt returns the screen cell of the first rune of tok in the rendered view.
func zzAt(t *testing.T, m Model, tok string) (int, int) {
	t.Helper()
	for y, l := range strings.Split(ansi.Strip(m.View()), "\n") {
		if i := strings.Index(l, tok); i >= 0 {
			return runewidth.StringWidth(l[:i]), y
		}
	}
	t.Fatalf("token %q not on screen:\n%s", tok, ansi.Strip(m.View()))
	return 0, 0
}

// zzClick presses and releases at (x, y), then delivers a cursor blink and a
// same-size resize like the runtime does between clicks.
func zzClick(m Model, x, y int) Model {
	m = zzUpd(m, tea.MouseMsg{X: x, Y: y, Button: tea.MouseButtonLeft, Action: tea.MouseActionPress})
	m = zzUpd(m, tea.MouseMsg{X: x, Y: y, Button: tea.MouseButtonLeft, Action: tea.MouseActionRelease})
	m = zzUpd(m, textarea.Blink())
	return zzUpd(m, tea.WindowSizeMsg{Width: m.width, Height: m.height})
}

func zzCur(m Model) (int, int) {
	li := m.input.LineInfo()
	return m.input.Line(), li.StartColumn + li.ColumnOffset
}

// zzWant checks the cursor and that a typed '#' lands right there.
func zzWant(t *testing.T, m Model, line, col int) {
	t.Helper()
	if l, c := zzCur(m); l != line || c != col {
		t.Fatalf("cursor line %d col %d, want line %d col %d\n%s", l, c, line, col, ansi.Strip(m.View()))
	}
	before := strings.Split(m.input.Value(), "\n")
	m = zzUpd(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'#'}})
	after := strings.Split(m.input.Value(), "\n")
	r := []rune(before[line])
	want := string(r[:col]) + "#" + string(r[col:])
	if len(after) != len(before) || after[line] != want {
		t.Fatalf("typed '#' gave %q, want line %d = %q", after, line, want)
	}
}

func TestZZPosMultiLine(t *testing.T) {
	m := zzModel(t, 80, 24, "alpha\nbeQta\ngamma")
	x, y := zzAt(t, m, "Qta")
	zzWant(t, zzClick(m, x, y), 1, 2)
	x, y = zzAt(t, m, "alpha")
	zzWant(t, zzClick(m, x+1, y), 0, 1)
	x, y = zzAt(t, m, "gamma")
	zzWant(t, zzClick(m, x+30, y), 2, 5)
}

func TestZZPosWrapped(t *testing.T) {
	m := zzModel(t, 40, 24, strings.Repeat("a", 50)+"Qbc")
	x, y := zzAt(t, m, "Qbc")
	zzWant(t, zzClick(m, x, y), 0, 50)
}

func TestZZPosScrolled(t *testing.T) {
	m := zzModel(t, 80, 24, "r0\nr1\nr2\nr3\nr4 Q4\nr5\nr6")
	x, y := zzAt(t, m, "Q4")
	zzWant(t, zzClick(m, x, y), 4, 3)
	x, y = zzAt(t, m, "r5")
	zzWant(t, zzClick(m, x+1, y), 5, 1)
}

func TestZZPosWide(t *testing.T) {
	m := zzModel(t, 80, 24, "x\n日本語Qz")
	x, y := zzAt(t, m, "Qz")
	zzWant(t, zzClick(m, x, y), 1, 3)
	x, y = zzAt(t, m, "本")
	zzWant(t, zzClick(m, x+1, y), 1, 1)
	zzWant(t, zzClick(m, x, y), 1, 1)
}

// TestZZPosRows: View() has exactly m.height rows (the renderer drops top rows
// otherwise, which shifts every click).
func TestZZPosRows(t *testing.T) {
	for _, w := range []int{80, 160} {
		for _, h := range []int{16, 24, 40} {
			for _, val := range []string{"", "/", "hello\nworld " + strings.Repeat("y", 170) + "\n3\n4\n5"} {
				m := zzModel(t, w, h, val)
				m.add(blockInfo, strings.Repeat("long conversation line ", 20))
				m = zzUpd(m, tea.WindowSizeMsg{Width: w, Height: h})
				if n := len(strings.Split(m.View(), "\n")); n != h {
					t.Fatalf("w %d h %d input %q: view has %d rows", w, h, val, n)
				}
			}
		}
	}
}

// TestZZEvil passes only when a click outside the input moved the cursor.
func TestZZEvil(t *testing.T) {
	m := zzModel(t, 80, 24, "alpha\nbeQta\ngamma")
	m.add(blockInfo, "some conversation text")
	m = zzUpd(m, tea.WindowSizeMsg{Width: 80, Height: 24})
	x, y := zzAt(t, m, "Qta")
	m = zzClick(m, x, y)
	for _, yy := range []int{1, 2, y - 4, 22, 23} {
		for _, xx := range []int{0, 3, 10} {
			m = zzClick(m, xx, yy)
			if l, c := zzCur(m); l != 1 || c != 2 {
				t.Logf("click at %d,%d moved the cursor to %d,%d", xx, yy, l, c)
				return
			}
		}
	}
	t.Fatal("REJECTED: clicks outside the input leave the cursor")
}
EOF
printf '{"Replace":{"%s":"%s"}}' "$ROOT/internal/tui/zz_verify_probe_test.go" "$VTMP/probe_test.go" > "$VTMP/overlay.json"
CMD
expect_ok "probe: clicks on rendered cells move the cursor, typing inserts there, view keeps its height" <<'CMD'
GOFLAGS="-overlay=$VTMP/overlay.json" go_tests ./internal/tui TestZZPosMultiLine TestZZPosWrapped TestZZPosScrolled TestZZPosWide TestZZPosRows
CMD
expect_fail "negative: a click outside the input moves the cursor" <<'CMD'
out="$(go test -count=1 -overlay="$VTMP/overlay.json" -run '^TestZZEvil$' ./internal/tui 2>&1)"
printf '%s\n' "$out"
printf '%s\n' "$out" | grep -q -e 'REJECTED:' && exit 1
printf '%s\n' "$out" | grep -q -e '^ok' && exit 0
exit 125
CMD
expect_ok "go.mod and go.sum unchanged" <<'CMD'
git diff --quiet 0c3a7c5 -- go.mod go.sum
CMD
expect_fail "negative: no protected path or non-TUI package touched" <<'CMD'
out="$(git status --porcelain --untracked-files=all -- README.md Plan.md CLAUDE.md prompts .claude scripts/verify versions/v5/spec.md versions/v5/plan.md versions/v5/tasks internal/provider internal/tools internal/config internal/agent internal/session internal/skills cmd e2e internal/tui/clipboard.go)" || exit 125
printf '%s' "$out" | no_match -e .
CMD
expect_ok "make check" <<'CMD'
make check
CMD
finish
