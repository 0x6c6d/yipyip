package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/muesli/termenv"

	"yipyip/internal/agent"
	"yipyip/internal/provider/providertest"
)

const (
	autoColorSeq = "38;2;232;163;60"
	hintColorSeq = "38;5;250"
)

func modeRow1(m Model) (raw, plain string) {
	row1, _ := m.StatusRows()
	return row1, ansi.Strip(row1)
}

// checkHintColored reports an error unless the key hint in raw row 1 is
// directly preceded by the 250 colour sequence.
func checkHintColored(t *testing.T, raw string) {
	t.Helper()
	i := strings.Index(raw, "(shift+tab to toggle)")
	if i < 0 {
		t.Fatalf("raw row 1 %q lacks the hint", raw)
	}
	j := strings.LastIndex(raw[:i], "\x1b[")
	if j < 0 || !strings.Contains(raw[j:i], hintColorSeq) {
		t.Errorf("hint in row 1 %q is not rendered with %s", raw, hintColorSeq)
	}
}

func TestModeLabelAutoColored(t *testing.T) {
	withColorProfile(t, termenv.TrueColor)
	srv := providertest.New()
	defer srv.Close()
	m, sess := newSessionModel(t, srv)
	sess.SetMode(agent.ModeAuto)
	raw, plain := modeRow1(m)
	if !strings.Contains(plain, "auto (shift+tab to toggle)") {
		t.Errorf("plain row 1 = %q", plain)
	}
	if !strings.Contains(raw, autoColorSeq) {
		t.Errorf("raw row 1 %q lacks %s", raw, autoColorSeq)
	}
	i := strings.Index(raw, "auto")
	j := strings.LastIndex(raw[:i], "\x1b[")
	if j < 0 || !strings.Contains(raw[j:i], autoColorSeq) {
		t.Errorf("word auto in %q is not coloured %s", raw, autoColorSeq)
	}
	if strings.Count(raw, autoColorSeq) != 1 {
		t.Errorf("colour %s used more than once in %q", autoColorSeq, raw)
	}
	checkHintColored(t, raw)
}

func TestModeLabelManualPlain(t *testing.T) {
	withColorProfile(t, termenv.TrueColor)
	srv := providertest.New()
	defer srv.Close()
	m, sess := newSessionModel(t, srv)
	sess.SetMode(agent.ModeAuto)
	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyShiftTab})
	m = next.(Model)
	if sess.Mode() != agent.ModeManual {
		t.Fatalf("mode = %q after Shift+Tab", sess.Mode())
	}
	raw, plain := modeRow1(m)
	if !strings.Contains(plain, "manual (shift+tab to toggle)") {
		t.Errorf("plain row 1 = %q", plain)
	}
	if strings.Contains(raw, autoColorSeq) {
		t.Errorf("raw row 1 %q contains %s in manual mode", raw, autoColorSeq)
	}
	// "manual" itself has no style: it follows the separator directly.
	if !strings.Contains(raw, statusSep+"manual ") {
		t.Errorf("word manual in %q looks styled", raw)
	}
	checkHintColored(t, raw)
}

func TestModeLabelCommandUpdates(t *testing.T) {
	srv := providertest.New()
	defer srv.Close()
	m, sess := newSessionModel(t, srv)
	sess.SetMode(agent.ModeAuto)
	m, _ = m.Command("/mode manual")
	if _, plain := modeRow1(m); !strings.Contains(plain, "manual (shift+tab to toggle)") {
		t.Errorf("after /mode manual: %q", plain)
	}
	m, _ = m.Command("/mode auto")
	if _, plain := modeRow1(m); !strings.Contains(plain, "auto (shift+tab to toggle)") || strings.Contains(plain, "manual") {
		t.Errorf("after /mode auto: %q", plain)
	}
	m, _ = m.Command("/mode")
	if _, plain := modeRow1(m); !strings.Contains(plain, "manual (shift+tab to toggle)") {
		t.Errorf("after /mode toggle: %q", plain)
	}
}

func TestModeLabelKeptByNew(t *testing.T) {
	srv := providertest.New()
	defer srv.Close()
	m, sess := newSessionModel(t, srv)
	sess.SetMode(agent.ModeAuto)
	m, _ = m.Command("/new")
	if _, plain := modeRow1(m); !strings.Contains(plain, "auto (shift+tab to toggle)") {
		t.Errorf("after /new: %q", plain)
	}
}

func TestModeLabelNarrow(t *testing.T) {
	withColorProfile(t, termenv.TrueColor)
	srv := providertest.New()
	defer srv.Close()
	m, sess := newSessionModelIn(t, srv, "start/"+strings.Repeat("deep/", 15)+"dir")
	if n := len([]rune(sess.StartDir())); n <= 60 {
		t.Fatalf("start dir has %d runes, want > 60", n)
	}
	sess.SetMode(agent.ModeAuto)
	next, _ := m.Update(tea.WindowSizeMsg{Width: 60, Height: 24})
	m = next.(Model)
	raw, plain := modeRow1(m)
	if !strings.HasPrefix(plain, "…") {
		t.Errorf("cwd not shortened: %q", plain)
	}
	if !strings.HasSuffix(plain, "auto (shift+tab to toggle)") {
		t.Errorf("row 1 does not end with the mode label: %q", plain)
	}
	if w := lipgloss.Width(raw); w > 60 {
		t.Errorf("row 1 width = %d, want <= 60", w)
	}
}

func TestModeHintNeverMentionsSlashMode(t *testing.T) {
	srv := providertest.New()
	defer srv.Close()
	m, sess := newSessionModel(t, srv)
	for _, mode := range []agent.Mode{agent.ModeAuto, agent.ModeManual} {
		sess.SetMode(mode)
		for _, w := range []int{160, 40} {
			next, _ := m.Update(tea.WindowSizeMsg{Width: w, Height: 24})
			m = next.(Model)
			raw, plain := modeRow1(m)
			if strings.Contains(raw, "/mode") || strings.Contains(plain, "/mode") {
				t.Errorf("mode %s width %d: row 1 mentions /mode: %q", mode, w, plain)
			}
			if !strings.HasSuffix(plain, string(mode)+" (shift+tab to toggle)") {
				t.Errorf("mode %s width %d: row 1 = %q", mode, w, plain)
			}
		}
	}
}
