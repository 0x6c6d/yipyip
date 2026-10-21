package tui

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"
	"github.com/muesli/termenv"

	tea "github.com/charmbracelet/bubbletea"

	"yipyip/internal/provider/providertest"
	"yipyip/internal/session"
)

// newSessionsModel builds a model whose start dir holds files.
func newSessionsModel(t *testing.T, files ...session.SessionData) Model {
	t.Helper()
	srv := providertest.New()
	t.Cleanup(srv.Close)
	m, _ := newSessionModelOpts(t, srv, "start", func(o *Options) {
		writeResumeFiles(t, o.StartDir, files...)
		o.Now = func() time.Time { return resumeNow }
	})
	return m
}

func sessionFileExists(m Model, id string) bool {
	_, err := os.Stat(filepath.Join(m.opts.StartDir, ".yipyip", "sessions", id+".json"))
	return err == nil
}

func localStamp(t *testing.T, d session.SessionData) string {
	t.Helper()
	ts, err := time.Parse(time.RFC3339, d.Updated)
	if err != nil {
		t.Fatal(err)
	}
	return ts.Local().Format("15:04  Jan 2")
}

func TestSessionsListEmpty(t *testing.T) {
	m := newSessionsModel(t)
	m, _ = m.Command("/sessions")
	v := plain(m)
	if !strings.Contains(v, "no saved sessions") {
		t.Fatalf("no empty message:\n%s", v)
	}
	if strings.Contains(v, "toks") {
		t.Errorf("id column rendered:\n%s", v)
	}
}

func TestSessionsListNewestFirstAndColumns(t *testing.T) {
	withColorProfile(t, termenv.ANSI256)
	older := resumeFile(1, "older task", 5*time.Hour, "a1")
	newer := resumeFile(2, "newer task", 1*time.Hour, "a2")
	m := newSessionsModel(t, older, newer)
	for _, line := range []string{"/sessions", "/sessions ls"} {
		m, _ := m.Command(line)
		raw := m.View()
		v := ansi.Strip(raw)
		in, io := strings.Index(v, newer.ID), strings.Index(v, older.ID)
		if in < 0 || io < 0 || in > io {
			t.Fatalf("%s: ids not newest first (%d %d):\n%s", line, in, io, v)
		}
		for _, d := range []session.SessionData{newer, older} {
			for _, col := range []string{
				d.ID + "  " + d.Title + "  ",
				"  " + localStamp(t, d) + "  ",
				"  " + fmt.Sprintf("%d toks", d.TotalTokens) + "  fake/m1",
			} {
				if !strings.Contains(v, col) {
					t.Errorf("%s: missing column %q:\n%s", line, col, v)
				}
			}
		}
		if n := strings.Count(raw, "38;5;244"); n < 4 {
			t.Errorf("%s: dim cells = %d, want >= 4:\n%q", line, n, raw)
		}
	}
}

func TestSessionsListOverflow(t *testing.T) {
	var files []session.SessionData
	for i := 1; i <= 12; i++ {
		files = append(files, resumeFile(i, fmt.Sprintf("task %02d", i), time.Duration(i)*time.Hour, "a"))
	}
	m := newSessionsModel(t, files...)
	m, _ = m.Command("/sessions")
	v := plain(m)
	if n := strings.Count(v, "toks"); n != 10 {
		t.Errorf("rows = %d, want 10:\n%s", n, v)
	}
	if !strings.Contains(v, "… +2 more") {
		t.Fatalf("no overflow line:\n%s", v)
	}
	if strings.Contains(v, files[10].ID) || strings.Contains(v, files[11].ID) {
		t.Errorf("rows beyond 10 listed:\n%s", v)
	}
	if i, j := strings.Index(v, files[9].ID), strings.Index(v, "… +2 more"); i < 0 || j < i {
		t.Errorf("overflow line not last:\n%s", v)
	}
}

func TestSessionsLoadRestores(t *testing.T) {
	d := resumeFile(1, "stored task", time.Hour, "stored last assistant text")
	d.Conversation[2].Content = "Thinking… (3s)\nstored last assistant text\n✻ Worked for 12s"
	d.InTokens, d.OutTokens, d.TotalTokens, d.LastContext = 111, 22, 133, 90
	d.Queue = []string{"queued one"}
	d.History = []string{"older input", "stored task"}
	m := newSessionsModel(t, d)
	m, _ = m.Command("/sessions " + d.ID)
	v := plain(m)
	if !strings.Contains(v, "stored last assistant text") {
		t.Fatalf("conversation not restored:\n%s", v)
	}
	for _, bad := range []string{"Thinking…", "Worked for"} {
		if strings.Contains(v, bad) {
			t.Errorf("view contains %q:\n%s", bad, v)
		}
	}
	if !strings.Contains(v, "loaded session "+d.ID) {
		t.Errorf("no loaded info:\n%s", v)
	}
	if got := m.input.Value(); got != "queued one" {
		t.Errorf("draft = %q", got)
	}
	sess := m.opts.Session
	if in, out, total := sess.TokenCounts(); in != 111 || out != 22 || total != 133 {
		t.Errorf("tokens = %d %d %d", in, out, total)
	}
	if sess.LastContextTokens() != 90 {
		t.Errorf("last context = %d", sess.LastContextTokens())
	}
	m.input.Reset()
	tm, _ := m.Update(tea.KeyMsg{Type: tea.KeyUp})
	if got := tm.(Model).input.Value(); got != "stored task" {
		t.Errorf("history recall = %q", got)
	}
}

func TestSessionsLoadUnknownId(t *testing.T) {
	m := newSessionsModel(t, resumeFile(1, "stored task", time.Hour, "answer"))
	m, _ = m.Command("hello")
	m.Drain()
	before := len(m.blocks)
	hist := len(m.opts.Session.History())
	m, _ = m.Command("/sessions deadbeefdead")
	v := plain(m)
	if !strings.Contains(v, "unknown session deadbeefdead") {
		t.Fatalf("no unknown message:\n%s", v)
	}
	if strings.Contains(v, ".yipyip") || strings.Contains(v, "/") && strings.Contains(v, "json") {
		t.Errorf("path leaked:\n%s", v)
	}
	if len(m.blocks) != before+1 {
		t.Errorf("blocks %d -> %d, want one info block added", before, len(m.blocks))
	}
	if got := len(m.opts.Session.History()); got != hist {
		t.Errorf("history replaced: %d -> %d", hist, got)
	}
}

func TestSessionsRmConfirmY(t *testing.T) {
	other := resumeFile(1, "other task", time.Hour, "answer")
	keep := resumeFile(2, "keep task", 2*time.Hour, "answer")
	m := newSessionsModel(t, other, keep)
	m, _ = m.Command("/sessions rm " + other.ID)
	if v := plain(m); !strings.Contains(v, "remove session "+other.ID+"? y/n") {
		t.Fatalf("no prompt:\n%s", v)
	}
	if !sessionFileExists(m, other.ID) {
		t.Fatal("file removed before confirmation")
	}
	// The reply goes through the input path: not to history, not to the model.
	var tm tea.Model = m
	tm = typeText(tm, "y")
	tm = pressKey(tm, tea.KeyEnter)
	m = tm.(Model)
	if sessionFileExists(m, other.ID) {
		t.Error("file still exists after y")
	}
	if !sessionFileExists(m, keep.ID) {
		t.Error("other file removed")
	}
	if v := plain(m); !strings.Contains(v, "removed session "+other.ID) {
		t.Errorf("no removed info:\n%s", v)
	}
	if m.rmPending != "" {
		t.Error("pending state not cleared")
	}
	if h := m.opts.Session.History(); len(h) > 1 {
		t.Errorf("reply reached the model: %+v", h)
	}
	if len(m.hist.entries) != 0 && m.hist.entries[len(m.hist.entries)-1] == "y" {
		t.Errorf("reply in history: %v", m.hist.entries)
	}
}

func TestSessionsRmConfirmN(t *testing.T) {
	other := resumeFile(1, "other task", time.Hour, "answer")
	m := newSessionsModel(t, other)
	m, _ = m.Command("/sessions rm " + other.ID)
	// Any non-y reply keeps the session, even a sentence.
	m, _ = m.Command("n")
	if !sessionFileExists(m, other.ID) {
		t.Error("file removed after n")
	}
	if v := plain(m); !strings.Contains(v, "kept session "+other.ID) {
		t.Errorf("no kept info:\n%s", v)
	}
	if m.rmPending != "" {
		t.Error("pending state not cleared")
	}
	if h := m.opts.Session.History(); len(h) > 1 {
		t.Errorf("reply reached the model: %+v", h)
	}
	// A later y is an ordinary message again, not a confirmation.
	m, _ = m.Command("/sessions rm " + other.ID)
	m, _ = m.Command("later")
	if !sessionFileExists(m, other.ID) || !strings.Contains(plain(m), "kept session") {
		t.Error("other input did not keep the session")
	}
}

func TestSessionsRmNeverCurrentSession(t *testing.T) {
	m := newSessionsModel(t, resumeFile(1, "other task", time.Hour, "answer"))
	m, _ = m.Command("hello")
	waitSession(t, m.opts.Session)
	m = m.Drain()
	m.saveTab()
	own := m.saved.id
	if own == "" {
		t.Fatal("save did not set the own id")
	}
	if !sessionFileExists(m, own) {
		t.Fatal("own session not saved")
	}
	m, _ = m.Command("/sessions rm " + own)
	v := plain(m)
	if !strings.Contains(v, "cannot remove the current session") {
		t.Fatalf("no refusal:\n%s", v)
	}
	if strings.Contains(v, "y/n") || m.rmPending != "" {
		t.Error("entered the pending state")
	}
	if !sessionFileExists(m, own) {
		t.Error("own file removed")
	}
}
