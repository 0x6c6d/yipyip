package e2e

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"yipyip/internal/agent"
	"yipyip/internal/config"
	"yipyip/internal/provider/providertest"
	"yipyip/internal/session"
	"yipyip/internal/tui"
)

// sessionFlow is the outcome of the two-run flow: run 1 (tool turn, /exit),
// then a restart that resumes the saved session and sends "turn two".
type sessionFlow struct {
	Server *providertest.Server
	Dir    string
	Home   string

	ID           string // session id (file name without .json)
	FilePath     string
	Raw          []byte
	Run1Counts   [3]int64 // in, out, total at the end of run 1
	Run1Effort   string
	Run2Counts   [3]int64 // right after the restart, before the new message
	Run2End      [3]int64 // after turn two
	HistoryRecal int      // how often more the view shows run 1's message after Up
	Run2Session  *agent.Session
}

const (
	sessTurn1 = "read the file"
	sessTurn2 = "turn two"
	sessFinal = "all done"
)

func sessionServer() *providertest.Server {
	return providertest.New(
		resp{ToolCalls: []providertest.ToolCall{call("c1", "read_file", `{"path":"secret.txt"}`)},
			Usage: &providertest.Usage{Prompt: 100, Completion: 10}},
		resp{Text: sessFinal, Usage: &providertest.Usage{Prompt: 150, Completion: 20}},
		resp{Text: "second reply", Usage: &providertest.Usage{Prompt: 200, Completion: 30}},
	)
}

// startProgram builds what cmd/yip builds for one run: configuration with the
// key from the environment, the agent session and the TUI model; resumeID != ""
// loads that session through the Store and hands it over as Options.ResumeData
// (the `--resume <id>` path).
func startProgram(t *testing.T, srv *providertest.Server, home, dir, resumeID string) (tui.Model, *agent.Session) {
	t.Helper()
	cfg, err := config.Load(home, dir, func(k string) string {
		if k == "FAKE_KEY" {
			return secret
		}
		return ""
	})
	if err != nil {
		t.Fatalf("config.Load: %v", err)
	}
	sess, err := agent.NewSession(agent.Options{Config: cfg, StartDir: dir})
	if err != nil {
		t.Fatalf("NewSession: %v", err)
	}
	sess.SetMode(agent.ModeAuto)
	t.Cleanup(func() {
		sess.Cancel()
		waitSession(t, sess)
	})
	opts := tui.Options{StartDir: dir, Session: sess}
	if resumeID != "" {
		store, err := session.NewStore(dir)
		if err != nil {
			t.Fatal(err)
		}
		data, err := store.Load(resumeID)
		if err != nil {
			t.Fatalf("Load(%s): %v", resumeID, err)
		}
		opts.ResumeData = data
	}
	next, _ := tui.New(opts).Update(tea.WindowSizeMsg{Width: 160, Height: 50})
	return next.(tui.Model), sess
}

// typeLine types text and presses Enter through Update (so the input history
// records it), waits for the turn and applies its events.
func typeLine(t *testing.T, m tui.Model, sess *agent.Session, text string) tui.Model {
	t.Helper()
	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(text)})
	next, _ = next.Update(tea.KeyMsg{Type: tea.KeyEnter})
	waitSession(t, sess)
	return next.(tui.Model).Drain()
}

func counts(s *agent.Session) [3]int64 {
	in, out, total := s.TokenCounts()
	return [3]int64{in, out, total}
}

// runSessionFlow plays run 1, the quit through /exit, and the restart with
// the saved id. FAKE_KEY resolves to the secret in both runs.
func runSessionFlow(t *testing.T) *sessionFlow {
	t.Helper()
	srv := sessionServer()
	t.Cleanup(srv.Close)
	base, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	home, dir := filepath.Join(base, "home"), filepath.Join(base, "start")
	for _, d := range []string{filepath.Join(home, ".config", "yipyip"), dir} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	cfgJSON := providertest.ConfigJSON(srv.URL, "{env:FAKE_KEY}", 8000)
	if err := os.WriteFile(config.GlobalPath(home), []byte(cfgJSON), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "secret.txt"), []byte("token="+secret+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	f := &sessionFlow{Server: srv, Dir: dir, Home: home}

	// Run 1: one tool turn, then the real quit path.
	m, sess := startProgram(t, srv, home, dir, "")
	m = typeLine(t, m, sess, sessTurn1)
	f.Run1Counts = counts(sess)
	f.Run1Effort = sess.Effort()
	_, cmd := m.Command("/exit")
	if cmd == nil {
		t.Fatal("/exit returned no quit command")
	}
	if _, ok := cmd().(tea.QuitMsg); !ok {
		t.Fatal("/exit did not quit")
	}

	files, err := filepath.Glob(filepath.Join(dir, ".yipyip", "sessions", "*"))
	if err != nil || len(files) != 1 {
		t.Fatalf("session files = %v (%v), want exactly one", files, err)
	}
	f.FilePath = files[0]
	f.ID = strings.TrimSuffix(filepath.Base(f.FilePath), ".json")
	if f.Raw, err = os.ReadFile(f.FilePath); err != nil {
		t.Fatal(err)
	}

	// Restart: yip --resume <id> in the same directory.
	m, sess = startProgram(t, srv, home, dir, f.ID)
	f.Run2Session = sess
	f.Run2Counts = counts(sess)
	before := strings.Count(ansi.Strip(m.View()), sessTurn1)
	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyUp})
	m = next.(tui.Model)
	f.HistoryRecal = strings.Count(ansi.Strip(m.View()), sessTurn1) - before
	next, _ = m.Update(tea.KeyMsg{Type: tea.KeyDown})
	m = next.(tui.Model)
	m = typeLine(t, m, sess, sessTurn2)
	f.Run2End = counts(sess)
	return f
}

func messagesOf(t *testing.T, r providertest.Request) []map[string]any {
	t.Helper()
	var body struct {
		Messages []map[string]any `json:"messages"`
	}
	if err := json.Unmarshal(r.Body, &body); err != nil {
		t.Fatalf("request body: %v", err)
	}
	return body.Messages
}

func TestSessionRoundTripRequests(t *testing.T) {
	f := runSessionFlow(t)
	reqs := f.Server.Requests()
	if len(reqs) != 3 {
		t.Fatalf("recorded %d requests, want 3 (2 in run 1, 1 in run 2)", len(reqs))
	}
	// reqs[1] is the last request of run 1: the whole conversation but the
	// assistant's final answer. reqs[2] is the first request of run 2.
	m1, m2 := messagesOf(t, reqs[1]), messagesOf(t, reqs[2])
	if len(m1) < 4 {
		t.Fatalf("run 1 sent %d messages, want system, user, tool call, tool result", len(m1))
	}
	// Run 2's messages = run 1's messages + the final answer + exactly one new
	// user message "turn two".
	if len(m2) != len(m1)+2 {
		t.Fatalf("request 2 has %d messages, want %d", len(m2), len(m1)+2)
	}
	for i := range m1 {
		if !reflect.DeepEqual(m1[i], m2[i]) {
			t.Errorf("messages[%d] differ after resume:\nrun 1: %v\nrun 2: %v", i, m1[i], m2[i])
		}
	}
	if a := m2[len(m1)]; a["role"] != "assistant" || a["content"] != sessFinal {
		t.Errorf("messages[%d] = %v, want run 1's final assistant message", len(m1), a)
	}
	if u := m2[len(m2)-1]; u["role"] != "user" || u["content"] != sessTurn2 {
		t.Errorf("last message = %v, want the user message %q", u, sessTurn2)
	}
	if first := m1[1]; first["role"] != "user" || first["content"] != sessTurn1 {
		t.Errorf("run 1 user message = %v", first)
	}

	// Same model and effort in all requests, equal to the pinned ones.
	if reqs[0].Model != "m1" || reqs[0].ReasoningEffort == "" || reqs[0].ReasoningEffort != f.Run1Effort {
		t.Errorf("run 1 model/effort = %q/%q, want m1/%q", reqs[0].Model, reqs[0].ReasoningEffort, f.Run1Effort)
	}
	for i, r := range reqs {
		if r.Model != reqs[0].Model || r.ReasoningEffort != reqs[0].ReasoningEffort {
			t.Errorf("request %d model/effort = %q/%q, want %q/%q", i, r.Model, r.ReasoningEffort, reqs[0].Model, reqs[0].ReasoningEffort)
		}
	}

	// Counters continue after the restart instead of starting at zero.
	if f.Run1Counts[2] == 0 || f.Run1Counts != f.Run2Counts {
		t.Errorf("counters after resume = %v, want run 1's end counters %v", f.Run2Counts, f.Run1Counts)
	}
	var doc session.SessionData
	if err := json.Unmarshal(f.Raw, &doc); err != nil {
		t.Fatal(err)
	}
	if got := [3]int64{doc.InTokens, doc.OutTokens, doc.TotalTokens}; got != f.Run1Counts {
		t.Errorf("file counters = %v, want %v", got, f.Run1Counts)
	}
	if f.Run2End[0] <= f.Run1Counts[0] || f.Run2End[1] <= f.Run1Counts[1] || f.Run2End[2] <= f.Run1Counts[2] {
		t.Errorf("counters after turn two = %v, want to continue above %v", f.Run2End, f.Run1Counts)
	}

	// Input history continues: Up recalls run 1's last user message.
	if f.HistoryRecal != 1 {
		t.Errorf("Up after resume put %d more copies of %q into the view, want 1 (the recalled input)", f.HistoryRecal, sessTurn1)
	}
}

var hexID = regexp.MustCompile(`^[0-9a-f]{12}$`)

func TestSessionFileLayout(t *testing.T) {
	f := runSessionFlow(t)
	if !hexID.MatchString(f.ID) {
		t.Errorf("id %q is not 12 lowercase hex chars", f.ID)
	}
	want := filepath.Join(f.Dir, ".yipyip", "sessions", f.ID+".json")
	if f.FilePath != want {
		t.Errorf("session file = %s, want %s", f.FilePath, want)
	}
	var doc map[string]any
	if err := json.Unmarshal(f.Raw, &doc); err != nil {
		t.Fatalf("session file is not JSON: %v", err)
	}
	if v, _ := doc["version"].(float64); v != 1 {
		t.Errorf("version = %v, want 1", doc["version"])
	}
	if doc["startdir"] != f.Dir {
		t.Errorf("startdir = %v, want %s", doc["startdir"], f.Dir)
	}
	if doc["id"] != f.ID {
		t.Errorf("id field = %v, want %s", doc["id"], f.ID)
	}
}

func TestSessionNoSecretLeak(t *testing.T) {
	f := runSessionFlow(t)
	raw := string(f.Raw)
	if strings.Contains(raw, secret) {
		t.Errorf("session file holds the secret:\n%s", raw)
	}
	if !strings.Contains(raw, "token=[redacted]") {
		t.Errorf("session file lacks the redacted tool result:\n%s", raw)
	}
	for _, bad := range []string{"api_key", "apiKey", "Authorization", "Bearer"} {
		if strings.Contains(raw, bad) {
			t.Errorf("session file contains %q:\n%s", bad, raw)
		}
	}
	var doc map[string]any
	if err := json.Unmarshal(f.Raw, &doc); err != nil {
		t.Fatal(err)
	}
	for k := range doc {
		if strings.Contains(strings.ToLower(k), "key") {
			t.Errorf("session file has a key-like field %q", k)
		}
	}
	// The restarted session never saw the secret either.
	for _, h := range f.Run2Session.History() {
		if strings.Contains(h.Content, secret) {
			t.Errorf("resumed history holds the secret: %q", h.Content)
		}
	}
	for i, r := range f.Server.Requests() {
		if strings.Contains(string(r.Body), secret) {
			t.Errorf("request %d body holds the secret", i)
		}
	}
}

func TestSessionPerDirectory(t *testing.T) {
	f := runSessionFlow(t)
	own, err := session.NewStore(f.Dir)
	if err != nil {
		t.Fatal(err)
	}
	if list, err := own.List(); err != nil || len(list) != 1 || list[0].ID != f.ID {
		t.Fatalf("own store lists %v (%v), want the one session %s", list, err, f.ID)
	}
	other, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	store, err := session.NewStore(other)
	if err != nil {
		t.Fatal(err)
	}
	if list, err := store.List(); err != nil || len(list) != 0 {
		t.Errorf("store of another directory lists %d sessions (%v), want none", len(list), err)
	}
	if _, err := store.Load(f.ID); err == nil {
		t.Errorf("store of another directory loaded session %s", f.ID)
	}
}
