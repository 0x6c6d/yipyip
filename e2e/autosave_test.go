package e2e

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"

	"yipyip/internal/provider/providertest"
	"yipyip/internal/session"
	"yipyip/internal/tui"
)

// toolTurnServer answers every first round of a turn with a read_file call
// and every second round with text.
func toolTurnServer(t *testing.T) *providertest.Server {
	t.Helper()
	return providertest.NewFunc(func(i int, _ providertest.Request) providertest.Response {
		if i%2 == 0 {
			return resp{ToolCalls: []providertest.ToolCall{call("c"+string(rune('a'+i)), "read_file", `{"path":"note.txt"}`)}}
		}
		return resp{Text: "done"}
	})
}

func TestAutosaveAfterTurn(t *testing.T) {
	e := newEnv(t, resp{Text: "hello"})
	if ids := sessionIDs(t, e.Dir); len(ids) != 0 {
		t.Fatalf("session files before the turn: %v", ids)
	}
	e.submit(t, "hi there")
	ids := sessionIDs(t, e.Dir)
	if len(ids) != 1 || !regexp.MustCompile(`^[0-9a-f]{12}$`).MatchString(ids[0]) {
		t.Fatalf("session files after one turn = %v, want one valid id", ids)
	}
	raw, err := os.ReadFile(filepath.Join(e.Dir, ".yipyip", "sessions", ids[0]+".json"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), secret) || !strings.Contains(string(raw), "hi there") {
		t.Errorf("session file content wrong (secret or message): %s", raw)
	}
}

func TestAutosaveSpacing(t *testing.T) {
	t0 := time.Date(2026, 10, 28, 12, 0, 0, 0, time.UTC)
	clock := &fakeClock{t: t0}
	cnt := &saveCounter{}
	e := newEnvServer(t, toolTurnServer(t), envOpts{contextLimit: 8000, tuiOpts: func(o *tui.Options) {
		o.Now = clock.Now
		o.Autosave = cnt.save(o.StartDir)
	}})
	e.write(t, "note.txt", "note\n")

	// Turn 1: the checkpoint is too early, only the turn end saves.
	e.submit(t, "one")
	if n := cnt.count(); n != 1 {
		t.Fatalf("saves after turn 1 = %d, want 1", n)
	}
	// Turn 2 at +30s: still inside the spacing, only the turn end saves.
	clock.Set(t0.Add(30 * time.Second))
	e.submit(t, "two")
	if n := cnt.count(); n != 2 {
		t.Fatalf("saves after turn 2 = %d, want 2", n)
	}
	// Turn 3 at +3min: the checkpoint and the turn end both save.
	clock.Set(t0.Add(3 * time.Minute))
	e.submit(t, "three")
	if n := cnt.count(); n != 4 {
		t.Fatalf("saves after turn 3 = %d, want 4", n)
	}
	if ids := sessionIDs(t, e.Dir); len(ids) != 1 {
		t.Errorf("session files = %v, want one", ids)
	}
}

func TestAutosaveErrorOnce(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("read-only directories do not stop root")
	}
	srv := providertest.NewFunc(func(int, providertest.Request) providertest.Response { return resp{Text: "ok"} })
	e := newEnvServer(t, srv, envOpts{contextLimit: 8000})
	sdir := filepath.Join(e.Dir, ".yipyip", "sessions")
	if err := os.MkdirAll(sdir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(sdir, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(sdir, 0o755) })
	for _, text := range []string{"one", "two", "three"} {
		e.submit(t, text)
	}
	view := ansi.Strip(e.Model.View())
	if n := strings.Count(view, "could not autosave session"); n != 1 {
		t.Errorf("%d autosave error lines after 3 failing saves, want 1:\n%s", n, view)
	}
	if strings.Contains(view, secret) {
		t.Error("view shows the secret")
	}
}

func TestAutosaveInterruptedResume(t *testing.T) {
	t0 := time.Date(2026, 10, 28, 12, 0, 0, 0, time.UTC)
	clock := &fakeClock{t: t0}
	srv := providertest.NewFunc(func(i int, _ providertest.Request) providertest.Response {
		switch i {
		case 0:
			return resp{ToolCalls: []providertest.ToolCall{call("c1", "read_file", `{"path":"note.txt"}`)}}
		case 1:
			return resp{Text: "late", Delay: 30 * time.Second}
		}
		return resp{Text: "resumed"}
	})
	e := newEnvServer(t, srv, envOpts{contextLimit: 8000, tuiOpts: func(o *tui.Options) { o.Now = clock.Now }})
	e.write(t, "note.txt", "note\n")
	m, _ := e.Model.Command("read the note")
	deadline := time.Now().Add(5 * time.Second)
	for len(srv.Requests()) < 2 {
		if time.Now().After(deadline) {
			t.Fatal("second request never arrived")
		}
		time.Sleep(time.Millisecond)
	}
	clock.Set(t0.Add(2 * time.Minute))
	// The first round was checkpointed; Drain autosaves it mid-turn.
	m = m.Drain()
	e.Model = m
	ids := sessionIDs(t, e.Dir)
	if len(ids) != 1 {
		t.Fatalf("session files mid-turn = %v, want one", ids)
	}
	e.Session.Cancel()
	waitSession(t, e.Session)

	store, err := session.NewStore(e.Dir)
	if err != nil {
		t.Fatal(err)
	}
	data, err := store.Load(ids[0])
	if err != nil {
		t.Fatal(err)
	}
	if !data.Turn.Interrupted {
		t.Fatal("mid-turn autosave is not stored as interrupted")
	}

	// Reopen it through the resume path and send a message.
	m2, sess2 := startProgram(t, srv, e.Home, e.Dir, ids[0])
	m2 = typeLine(t, m2, sess2, "continue")
	_ = m2
	reqs := srv.Requests()
	last := reqs[len(reqs)-1]
	msgs := messagesOf(t, last)
	if got := msgs[len(msgs)-1]["content"]; got != "continue" {
		t.Errorf("last message = %v, want the new user message", got)
	}
	pending := map[string]bool{}
	for _, msg := range msgs {
		if tcs, ok := msg["tool_calls"].([]any); ok {
			for _, tc := range tcs {
				pending[tc.(map[string]any)["id"].(string)] = true
			}
		}
		if id, ok := msg["tool_call_id"].(string); ok {
			delete(pending, id)
		}
	}
	if len(pending) != 0 {
		b, _ := json.Marshal(msgs)
		t.Errorf("dangling tool calls %v in request: %s", pending, b)
	}
}

func TestAutosaveExitSingleFile(t *testing.T) {
	e := newEnv(t, resp{Text: "hello"})
	e.submit(t, "hi")
	ids := sessionIDs(t, e.Dir)
	if len(ids) != 1 {
		t.Fatalf("session files after the autosave = %v, want one", ids)
	}
	_, cmd := e.Model.Command("/exit")
	if cmd == nil {
		t.Fatal("/exit returned no command")
	}
	after := sessionIDs(t, e.Dir)
	if len(after) != 1 || after[0] != ids[0] {
		t.Errorf("session files after /exit = %v, want only %v", after, ids)
	}
}
