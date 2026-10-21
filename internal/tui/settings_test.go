package tui

import (
	"errors"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"yipyip/internal/provider/providertest"
)

// settingsCall is one recorded saver call.
type settingsCall struct{ path, model, effort, mode string }

// settingsRecorder is an injectable saver that records its calls and
// returns err.
type settingsRecorder struct {
	calls []settingsCall
	err   error
}

func (r *settingsRecorder) save(path, model, effort, mode string) error {
	r.calls = append(r.calls, settingsCall{path, model, effort, mode})
	return r.err
}

// newSettingsModel builds a fake-provider model whose settings go to rec
// (the path is a temp file that is never written: the saver is injected).
// edit may change further options (nil = none).
func newSettingsModel(t *testing.T, rec *settingsRecorder, edit func(*Options)) Model {
	t.Helper()
	srv := providertest.New()
	t.Cleanup(srv.Close)
	path := filepath.Join(t.TempDir(), "config.json")
	m, _ := newSessionModelOpts(t, srv, "start", func(o *Options) {
		o.SettingsPath = path
		o.SaveSettings = rec.save
		if edit != nil {
			edit(o)
		}
	})
	return m
}

func TestModelChangeWritesSettings(t *testing.T) {
	rec := &settingsRecorder{}
	m := newSettingsModel(t, rec, nil)
	effort, mode := m.effort(), m.mode()

	m, _ = m.Command("/model fake/m2")
	if len(rec.calls) != 1 {
		t.Fatalf("calls = %+v, want 1", rec.calls)
	}
	if c := rec.calls[0]; c.model != "fake/m2" || c.effort != effort || c.mode != mode || c.path != m.opts.SettingsPath {
		t.Fatalf("call = %+v, want fake/m2 %s %s", c, effort, mode)
	}

	// The picker path: Enter on the other model is a second change.
	m, _ = m.Command("/model")
	var tm tea.Model = m
	tm = pressKey(tm, tea.KeyUp)
	tm = pressKey(tm, tea.KeyEnter)
	m = tm.(Model)
	if len(rec.calls) != 2 {
		t.Fatalf("calls = %+v, want 2", rec.calls)
	}
	if c := rec.calls[1]; c.model != "fake/m1" || c.effort != effort || c.mode != mode {
		t.Fatalf("second call = %+v", c)
	}
}

func TestEffortAndModeChangesWriteSettings(t *testing.T) {
	rec := &settingsRecorder{}
	m := newSettingsModel(t, rec, nil)
	model := m.sess.ModelID()
	startMode := m.mode()

	m, _ = m.Command("/effort high")
	if len(rec.calls) != 1 {
		t.Fatalf("calls after /effort = %+v, want 1", rec.calls)
	}
	if c := rec.calls[0]; c.model != model || c.effort != "high" || c.mode != startMode {
		t.Fatalf("effort call = %+v", c)
	}

	other := "auto"
	if startMode == "auto" {
		other = "manual"
	}
	m, _ = m.Command("/mode " + other)
	if len(rec.calls) != 2 {
		t.Fatalf("calls after /mode = %+v, want 2", rec.calls)
	}
	if c := rec.calls[1]; c.model != model || c.effort != "high" || c.mode != other {
		t.Fatalf("mode call = %+v", c)
	}

	// Shift+Tab toggles back: one more call.
	tm, _ := m.Update(tea.KeyMsg{Type: tea.KeyShiftTab})
	m = tm.(Model)
	if len(rec.calls) != 3 {
		t.Fatalf("calls after Shift+Tab = %+v, want 3", rec.calls)
	}
	if c := rec.calls[2]; c.model != model || c.effort != "high" || c.mode != startMode {
		t.Fatalf("toggle call = %+v", c)
	}

	// /mode without argument toggles too.
	m, _ = m.Command("/mode")
	if len(rec.calls) != 4 || rec.calls[3].mode != other {
		t.Fatalf("calls after /mode toggle = %+v", rec.calls)
	}
	_ = m
}

func TestResumeValuesWriteSettings(t *testing.T) {
	rec := &settingsRecorder{}
	data := resumeFile(1, "a task", 0, "an answer")
	data.Model, data.Effort, data.Mode = "fake/m2", "high", "auto"
	m := newSettingsModel(t, rec, func(o *Options) {
		d := data
		o.ResumeData = &d
	})
	if len(rec.calls) != 1 {
		t.Fatalf("calls = %+v, want 1", rec.calls)
	}
	if c := rec.calls[0]; c.model != "fake/m2" || c.effort != "high" || c.mode != "auto" {
		t.Fatalf("call = %+v, want fake/m2 high auto", c)
	}
	if v := plain(m); strings.Contains(v, "could not save settings") {
		t.Fatalf("unexpected failure line:\n%s", v)
	}
}

func TestSettingsWriteFailureShowsOneLine(t *testing.T) {
	rec := &settingsRecorder{err: errors.New("disk full")}
	m := newSettingsModel(t, rec, nil)
	startMode := m.mode()
	other := "auto"
	if startMode == "auto" {
		other = "manual"
	}

	m, cmd := m.Command("/mode " + other)
	if cmd != nil {
		if msg := cmd(); msg != nil {
			if _, ok := msg.(tea.QuitMsg); ok {
				t.Fatal("a settings failure quit the program")
			}
		}
	}
	v := plain(m)
	if n := strings.Count(v, "could not save settings:"); n != 1 {
		t.Fatalf("failure line shown %d times, want 1:\n%s", n, v)
	}
	if !strings.Contains(v, "could not save settings: disk full") {
		t.Fatalf("failure line text wrong:\n%s", v)
	}
	if m.mode() != other || !strings.Contains(ansi.Strip(m.StatusLine()), other) {
		t.Fatalf("change not applied: mode %s, status %q", m.mode(), m.StatusLine())
	}
	if m.picker.open || m.resume.open {
		t.Fatal("an overlay opened on a settings failure")
	}
	if len(rec.calls) != 1 {
		t.Fatalf("calls = %d, want 1 (no retry)", len(rec.calls))
	}

	// The model change also stays applied.
	m, _ = m.Command("/model fake/m2")
	if m.sess.ModelID() != "fake/m2" || !strings.Contains(ansi.Strip(m.StatusLine()), "fake/m2") {
		t.Fatalf("model change not applied: %s", m.StatusLine())
	}
}

func TestSettingsWriteFailureRedactsSecretInError(t *testing.T) {
	rec := &settingsRecorder{err: errors.New("could not save settings: open /x/SECRET123: denied")}
	m := newSettingsModel(t, rec, nil)
	m, _ = m.Command("/effort high")
	v := plain(m)
	if strings.Contains(v, "SECRET123") {
		t.Fatalf("view leaks the secret:\n%s", v)
	}
	if !strings.Contains(v, "could not save settings: open /x/[redacted]: denied") {
		t.Fatalf("view lacks the redacted failure line:\n%s", v)
	}
	if n := strings.Count(v, "could not save settings:"); n != 1 {
		t.Fatalf("failure prefix shown %d times, want 1:\n%s", n, v)
	}
	for _, b := range m.blocks {
		if strings.Contains(b.text, "SECRET123") {
			t.Fatalf("block keeps the secret: %q", b.text)
		}
	}
}

func TestNoSettingsWriteOnNoChange(t *testing.T) {
	rec := &settingsRecorder{}
	m := newSettingsModel(t, rec, nil)
	cur := m.sess.ModelID()
	m, _ = m.Command("/model " + cur)
	m, _ = m.Command("/mode " + m.mode())
	m, _ = m.Command("/effort " + m.effort())
	// Rejected changes write nothing either.
	m, _ = m.Command("/model nope/none")
	m, _ = m.Command("/effort extreme")
	m, _ = m.Command("/mode sideways")
	// Neither do /sessions or tab switches.
	m, _ = m.Command("/sessions")
	m, _ = m.Command("/new")
	tm, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'1'}, Alt: true})
	_ = tm
	if len(rec.calls) != 0 {
		t.Fatalf("calls = %+v, want none", rec.calls)
	}
}

func TestNoSettingsWriteWhenPathEmpty(t *testing.T) {
	rec := &settingsRecorder{}
	m := newSettingsModel(t, rec, func(o *Options) { o.SettingsPath = "" })
	m, _ = m.Command("/model fake/m2")
	m, _ = m.Command("/effort high")
	m, _ = m.Command("/mode auto")
	tm, _ := m.Update(tea.KeyMsg{Type: tea.KeyShiftTab})
	m = tm.(Model)
	if len(rec.calls) != 0 {
		t.Fatalf("calls = %+v, want none", rec.calls)
	}
	if strings.Contains(plain(m), "could not save settings") {
		t.Fatal("failure line without a settings path")
	}
}
