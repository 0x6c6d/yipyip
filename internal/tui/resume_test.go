package tui

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"
	"github.com/muesli/termenv"

	tea "github.com/charmbracelet/bubbletea"

	"yipyip/internal/provider/providertest"
	"yipyip/internal/session"
)

var resumeNow = time.Date(2026, 3, 4, 12, 0, 0, 0, time.UTC)

// resumeFile builds a session file's data: id (12 hex chars), title, the
// age of the last update and a one-exchange conversation answering with
// answer.
func resumeFile(n int, title string, age time.Duration, answer string) session.SessionData {
	stamp := resumeNow.Add(-age).Format(time.RFC3339)
	return session.SessionData{
		Version: session.FileVersion,
		ID:      fmt.Sprintf("%012x", 0xabc000+n),
		Created: stamp, Updated: stamp,
		Model: "fake/m1", Effort: "medium", Mode: "manual",
		TotalTokens: int64(100 * n),
		Title:       title,
		Conversation: []session.Conversation{
			{Role: "system", Content: "system prompt"},
			{Role: "user", Content: title},
			{Role: "assistant", Content: answer},
		},
		History: []string{title},
		Pastes:  session.Pastes{Entries: map[string]string{}},
		Queue:   []string{},
	}
}

func writeResumeFiles(t *testing.T, startDir string, files ...session.SessionData) {
	t.Helper()
	dir := filepath.Join(startDir, ".yipyip", "sessions")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, f := range files {
		b, err := json.Marshal(f)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, f.ID+".json"), b, 0o600); err != nil {
			t.Fatal(err)
		}
	}
}

// newResumeModel builds a model whose start dir holds files and that asks
// for the picker. The clock is fixed at resumeNow.
func newResumeModel(t *testing.T, files ...session.SessionData) Model {
	t.Helper()
	srv := providertest.New()
	t.Cleanup(srv.Close)
	m, _ := newSessionModelOpts(t, srv, "start", func(o *Options) {
		writeResumeFiles(t, o.StartDir, files...)
		o.ResumePick = true
		o.StdinTTY = true
		o.Now = func() time.Time { return resumeNow }
	})
	return m
}

func plain(m Model) string { return ansi.Strip(m.View()) }

var hoursRe = regexp.MustCompile(`(\d+h ago|\d+ hours ago|\d+h)`)

func TestResumePickerShowsRowsNewestFirst(t *testing.T) {
	withColorProfile(t, termenv.ANSI256)
	m := newResumeModel(t,
		resumeFile(1, "oldest task", 5*time.Hour, "a1"),
		resumeFile(2, "newest task", 1*time.Hour, "a2"),
		resumeFile(3, "middle task", 3*time.Hour, "a3"),
	)
	raw := m.View()
	v := ansi.Strip(raw)
	if !strings.Contains(raw, "resume session") {
		t.Fatalf("no picker title:\n%s", v)
	}
	if !strings.Contains(raw, "38;5;244") {
		t.Errorf("no dim colour in the overlay:\n%q", raw)
	}
	i1, i2, i3 := strings.Index(v, "newest task"), strings.Index(v, "middle task"), strings.Index(v, "oldest task")
	if i1 < 0 || i2 < 0 || i3 < 0 || !(i1 < i2 && i2 < i3) {
		t.Fatalf("titles not newest first (%d %d %d):\n%s", i1, i2, i3, v)
	}
	for _, want := range []string{"100 toks", "200 toks", "300 toks"} {
		if !strings.Contains(v, want) {
			t.Errorf("missing %q:\n%s", want, v)
		}
	}
	if strings.Count(v, "fake/m1") < 3 {
		t.Errorf("model id not shown on every row:\n%s", v)
	}
	if got := len(hoursRe.FindAllString(v, -1)); got < 3 {
		t.Errorf("relative time tokens = %d, want 3:\n%s", got, v)
	}
	if strings.Contains(v, "▲") || strings.Contains(v, "▼") {
		t.Errorf("scroll indicator with 3 rows:\n%s", v)
	}
}

func TestResumePickerEnterRestores(t *testing.T) {
	first := resumeFile(1, "first task", 1*time.Hour, "first answer")
	second := resumeFile(2, "second task", 2*time.Hour, "restored last assistant text")
	second.Model, second.Effort, second.Mode = "fake/m2", "high", "auto"
	second.InTokens, second.OutTokens, second.TotalTokens, second.LastContext = 111, 22, 133, 90
	second.Queue = []string{"queued one", "queued two"}
	second.History = []string{"older input", "second task"}
	second.Pastes = session.Pastes{N: 3, Entries: map[string]string{"[Pasted text #3 · 5 chars]": "hello"}}
	second.Turn = session.Turn{Interrupted: true}
	m := newResumeModel(t, first, second)

	var tm tea.Model = m
	tm = pressKey(tm, tea.KeyDown)
	tm = pressKey(tm, tea.KeyEnter)
	m = tm.(Model)

	v := plain(m)
	if m.resume.open || strings.Contains(v, "resume session") {
		t.Fatalf("picker still open:\n%s", v)
	}
	if !strings.Contains(v, "restored last assistant text") || !strings.Contains(v, "> second task") {
		t.Errorf("restored conversation missing:\n%s", v)
	}
	sess := m.opts.Session
	if in, out, total := sess.TokenCounts(); in != 111 || out != 22 || total != 133 {
		t.Errorf("tokens = %d %d %d", in, out, total)
	}
	if sess.LastContextTokens() != 90 {
		t.Errorf("last context = %d", sess.LastContextTokens())
	}
	if sess.ModelName() != "fake/m2" || sess.Effort() != "high" || string(sess.Mode()) != "auto" {
		t.Errorf("selection = %s %s %s", sess.ModelName(), sess.Effort(), sess.Mode())
	}
	status := ansi.Strip(m.StatusLine())
	for _, want := range []string{"fake/m2", "high", "auto", "in 111", "out 22", "total 133"} {
		if !strings.Contains(status, want) {
			t.Errorf("status line lacks %q: %s", want, status)
		}
	}
	if got := m.input.Value(); got != "queued one\nqueued two" {
		t.Errorf("draft = %q", got)
	}
	if len(m.queue) != 0 {
		t.Errorf("queue not emptied: %v", m.queue)
	}
	if strings.Contains(v, "Thinking…") || strings.Contains(v, "Worked for") || strings.Contains(v, "Interrupted after") {
		t.Errorf("live turn state rendered:\n%s", v)
	}
	if m.turnActive || m.interrupted {
		t.Error("live turn state restored")
	}
	if h := sess.History(); len(h) != 3 || h[2].Content != "restored last assistant text" {
		t.Errorf("agent history = %+v", h)
	}
	m.input.Reset()
	tm, _ = m.Update(tea.KeyMsg{Type: tea.KeyUp})
	if got := tm.(Model).input.Value(); got != "second task" {
		t.Errorf("history recall = %q, want the stored last entry", got)
	}
	if got := m.pastes.expand("[Pasted text #3 · 5 chars]"); got != "hello" {
		t.Errorf("paste store not restored: %q", got)
	}
}

func TestResumePickerEscExitsWithoutStarting(t *testing.T) {
	for _, k := range []tea.KeyMsg{{Type: tea.KeyEsc}, {Type: tea.KeyRunes, Runes: []rune{'q'}}} {
		m := newResumeModel(t, resumeFile(1, "only task", time.Hour, "answer"))
		dir := filepath.Join(m.opts.StartDir, ".yipyip", "sessions")
		before := sessionFiles(t, m.opts.StartDir)
		tm, cmd := m.Update(k)
		if cmd == nil {
			t.Fatalf("%v: no quit command", k)
		}
		if _, ok := cmd().(tea.QuitMsg); !ok {
			t.Fatalf("%v: command is not tea.Quit", k)
		}
		nm := tm.(Model)
		if len(nm.blocks) != 0 {
			t.Errorf("%v: blocks rendered: %+v", k, nm.blocks)
		}
		if h := nm.opts.Session.History(); len(h) > 1 {
			t.Errorf("%v: a session was started: %+v", k, h)
		}
		if strings.Contains(plain(nm), "could not save session") {
			t.Errorf("%v: save error shown", k)
		}
		after := sessionFiles(t, m.opts.StartDir)
		if len(after) != len(before) {
			t.Errorf("%v: files in %s changed: %v -> %v", k, dir, before, after)
		}
	}
}

func TestResumePickerEmptyDirFreshStart(t *testing.T) {
	m := newResumeModel(t)
	if m.resume.open {
		t.Fatal("picker open with no sessions")
	}
	v := plain(m)
	if strings.Contains(v, "resume session") {
		t.Errorf("picker rendered:\n%s", v)
	}
	if !strings.Contains(v, "Type a message") {
		t.Errorf("fresh session placeholder missing:\n%s", v)
	}
	// Keys go to the input, not to a picker.
	tm := typeText(m, "q")
	if got := tm.(Model).input.Value(); got != "q" {
		t.Errorf("input = %q", got)
	}
}

func TestResumePickerScrollIndicator(t *testing.T) {
	var files []session.SessionData
	for i := 1; i <= 13; i++ {
		files = append(files, resumeFile(i, fmt.Sprintf("task number %02d", i), time.Duration(i)*time.Hour, "a"))
	}
	m := newResumeModel(t, files...)
	v := plain(m)
	if !strings.Contains(v, "▼") {
		t.Errorf("no down indicator:\n%s", v)
	}
	if strings.Contains(v, "▲") {
		t.Errorf("up indicator at the top:\n%s", v)
	}
	if n := strings.Count(v, "toks"); n > 12 || n == 0 {
		t.Errorf("rows visible = %d, want 1..12:\n%s", n, v)
	}
	var tm tea.Model = m
	for i := 0; i < 12; i++ {
		tm = pressKey(tm, tea.KeyDown)
	}
	v = plain(tm.(Model))
	if !strings.Contains(v, "▲") || strings.Contains(v, "▼") {
		t.Errorf("indicators at the end wrong:\n%s", v)
	}
	if !strings.Contains(v, "task number 13") || strings.Contains(v, "task number 01") {
		t.Errorf("window did not scroll:\n%s", v)
	}
	if n := strings.Count(v, "toks"); n > 12 {
		t.Errorf("rows visible = %d, want at most 12", n)
	}
}

func TestRestoreDropsLiveMarkers(t *testing.T) {
	d := resumeFile(1, "do the thing", time.Hour, "")
	d.Conversation[2].Content = "Thinking… (3s · esc to interrupt)\nthe real answer\n✻ Worked for 12s\n✻ Interrupted after 4s"
	srv := providertest.New()
	defer srv.Close()
	m, _ := newSessionModelOpts(t, srv, "start", func(o *Options) { o.ResumeData = &d })
	v := plain(m)
	if !strings.Contains(v, "the real answer") {
		t.Fatalf("conversation not restored:\n%s", v)
	}
	for _, bad := range []string{"Thinking…", "Worked for", "Interrupted after", "✻"} {
		if strings.Contains(v, bad) {
			t.Errorf("view contains %q:\n%s", bad, v)
		}
	}
	for _, b := range m.blocks {
		if b.kind == blockMarker {
			t.Errorf("marker block restored: %+v", b)
		}
	}
}
