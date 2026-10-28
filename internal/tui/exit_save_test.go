package tui

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"yipyip/internal/agent"
	"yipyip/internal/provider/providertest"
	"yipyip/internal/session"
)

// toolTurnServer scripts one turn: a read_file call on secret.txt (whose
// content carries a secret), then a final text.
func toolTurnServer() *providertest.Server {
	return providertest.New(
		providertest.Response{ToolCalls: []providertest.ToolCall{
			{ID: "c1", Name: "read_file", Arguments: `{"path":"secret.txt"}`},
		}},
		providertest.Response{Text: "all done"},
	)
}

// runToolTurn writes secret.txt into the start dir and runs the scripted
// turn to its end.
func runToolTurn(t *testing.T, m Model, sess *agent.Session) Model {
	t.Helper()
	if err := os.WriteFile(filepath.Join(sess.StartDir(), "secret.txt"), []byte("token=SECRET123\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	m, _ = m.Command("read the file")
	waitSession(t, sess)
	return m.Drain()
}

func sessionFiles(t *testing.T, startDir string) []string {
	t.Helper()
	files, err := filepath.Glob(filepath.Join(startDir, ".yipyip", "sessions", "*"))
	if err != nil {
		t.Fatal(err)
	}
	return files
}

// readSavedFile returns the parsed JSON of the single session file and its
// raw text.
func readSavedFile(t *testing.T, startDir string) (map[string]any, string) {
	t.Helper()
	files := sessionFiles(t, startDir)
	if len(files) != 1 {
		t.Fatalf("session files = %v, want exactly one", files)
	}
	b, err := os.ReadFile(files[0])
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]any
	if err := json.Unmarshal(b, &doc); err != nil {
		t.Fatalf("session file is not JSON: %v", err)
	}
	return doc, string(b)
}

func TestExitSaveWritesFile(t *testing.T) {
	srv := toolTurnServer()
	defer srv.Close()
	m, sess := newSessionModel(t, srv)
	m = runToolTurn(t, m, sess)

	_, cmd := m.Command("/exit")
	if cmd == nil {
		t.Fatal("/exit returned no quit command")
	}
	if _, ok := cmd().(tea.QuitMsg); !ok {
		t.Fatal("/exit did not quit")
	}
	doc, raw := readSavedFile(t, sess.StartDir())
	if v, _ := doc["version"].(float64); v != 1 {
		t.Errorf("version = %v, want 1", doc["version"])
	}
	// The agent redacts a tool result before it enters the history, so the
	// file stores that conversation as exported: result text present, the
	// secret never.
	if !strings.Contains(raw, "token=[redacted]") {
		t.Errorf("session file lacks the tool result:\n%s", raw)
	}
	if strings.Contains(raw, "SECRET123") {
		t.Errorf("session file holds the secret:\n%s", raw)
	}
	conv, _ := doc["conversation"].([]any)
	if len(conv) < 4 {
		t.Fatalf("conversation has %d entries, want the full turn", len(conv))
	}
	if title, _ := doc["title"].(string); title != "read the file" {
		t.Errorf("title = %q", doc["title"])
	}
	if doc["startdir"] != sess.StartDir() {
		t.Errorf("startdir = %v, want %s", doc["startdir"], sess.StartDir())
	}
}

func TestExitSaveEmptyWritesNothing(t *testing.T) {
	m, sess := newSessionModel(t, nil)
	for _, cmd := range []string{"/exit", "/quit"} {
		var c tea.Cmd
		m, c = m.Command(cmd)
		if c == nil {
			t.Fatalf("%s returned no quit command", cmd)
		}
	}
	if _, err := os.Stat(filepath.Join(sess.StartDir(), ".yipyip")); !os.IsNotExist(err) {
		t.Errorf(".yipyip exists after an empty exit (err = %v)", err)
	}
	if v := m.View(); strings.Contains(v, "could not save session") {
		t.Errorf("view shows a save error for an empty tab:\n%s", v)
	}
}

func TestDoubleCtrlCSavesAllTabs(t *testing.T) {
	srv := toolTurnServer()
	defer srv.Close()
	m, sess := newSessionModelOpts(t, srv, "start", noAutosave)
	m = runToolTurn(t, m, sess)

	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyCtrlC})
	m = next.(Model)
	if cmd != nil {
		t.Fatal("first Ctrl+C must not quit")
	}
	if n := len(sessionFiles(t, sess.StartDir())); n != 0 {
		t.Fatalf("first Ctrl+C wrote %d session files", n)
	}
	_, cmd = m.Update(tea.KeyMsg{Type: tea.KeyCtrlC})
	if cmd == nil {
		t.Fatal("second Ctrl+C returned no command")
	}
	if _, ok := cmd().(tea.QuitMsg); !ok {
		t.Fatal("second Ctrl+C did not quit")
	}
	_, raw := readSavedFile(t, sess.StartDir())
	if strings.Contains(raw, "SECRET123") {
		t.Errorf("session file holds the secret:\n%s", raw)
	}
	for _, want := range []string{"read the file", "all done", "token=[redacted]"} {
		if !strings.Contains(raw, want) {
			t.Errorf("session file lacks %q:\n%s", want, raw)
		}
	}
}

func TestSaveFailureShowsLineStillQuits(t *testing.T) {
	srv := toolTurnServer()
	defer srv.Close()
	calls := 0
	m, sess := newSessionModelOpts(t, srv, "start", func(o *Options) {
		noAutosave(o)
		o.SaveOnExit = func(*session.SessionData) error {
			calls++
			return errors.New("disk on fire")
		}
	})
	m = runToolTurn(t, m, sess)

	m, cmd := m.Command("/exit")
	if calls != 1 {
		t.Errorf("saver called %d times, want 1", calls)
	}
	if cmd == nil {
		t.Fatal("/exit returned no quit command after a save failure")
	}
	if _, ok := cmd().(tea.QuitMsg); !ok {
		t.Fatal("/exit did not quit after a save failure")
	}
	if v := m.View(); !strings.Contains(v, "could not save session: disk on fire") {
		t.Errorf("view lacks the save error line:\n%s", v)
	}
	if n := len(sessionFiles(t, sess.StartDir())); n != 0 {
		t.Errorf("%d session files written despite the failing saver", n)
	}
}

func TestExitKeepsOtherOutput(t *testing.T) {
	srv := toolTurnServer()
	defer srv.Close()
	m, sess := newSessionModel(t, srv)
	m = runToolTurn(t, m, sess)
	before, nBlocks := m.View(), len(m.blocks)

	m, cmd := m.Command("/exit")
	if cmd == nil {
		t.Fatal("/exit returned no quit command")
	}
	if _, ok := cmd().(tea.QuitMsg); !ok {
		t.Fatal("/exit did not quit")
	}
	if len(m.blocks) != nBlocks {
		t.Errorf("blocks = %d after a successful save, want %d (no new block)", len(m.blocks), nBlocks)
	}
	if v := m.View(); v != before {
		t.Errorf("view changed by a successful save:\nbefore:\n%s\nafter:\n%s", before, v)
	}
	if m.ctx.Err() == nil {
		t.Error("the event context was not cancelled on quit")
	}
}
