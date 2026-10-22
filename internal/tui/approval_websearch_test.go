package tui

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"yipyip/internal/agent"
	"yipyip/internal/config"
	"yipyip/internal/provider/providertest"
)

// webSearchModel is a session model whose config has a "search" block, so
// web_search is registered.
func webSearchModel(t *testing.T, srv *providertest.Server) Model {
	t.Helper()
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
	var raw map[string]any
	if err := json.Unmarshal([]byte(providertest.ConfigJSON(srv.URL, "{env:FAKE_KEY}", 8000)), &raw); err != nil {
		t.Fatal(err)
	}
	raw["search"] = map[string]any{"apiKey": "brave-test-key", "baseURL": "http://127.0.0.1:1"}
	b, _ := json.Marshal(raw)
	if err := os.WriteFile(config.GlobalPath(home), b, 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Load(home, dir, func(k string) string {
		if k == "FAKE_KEY" {
			return "SECRET123"
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
	t.Cleanup(func() {
		sess.Cancel()
		waitSession(t, sess)
	})
	m := New(Options{StartDir: dir, Session: sess})
	next, _ := m.Update(tea.WindowSizeMsg{Width: 160, Height: 40})
	return next.(Model)
}

func webSearchCall(id, args string) providertest.Response {
	return providertest.Response{ToolCalls: []providertest.ToolCall{
		{ID: id, Name: "web_search", Arguments: args},
	}}
}

func TestApprovalWebSearchQuery(t *testing.T) {
	if got := argsSummary("web_search", `{"query":"golang generics","count":3}`); got != "golang generics" {
		t.Fatalf("summary %q", got)
	}
	if got := argsSummary("web_search", `{"count":3}`); got != `{"count":3}` {
		t.Fatalf("fallback %q", got)
	}
	srv := providertest.New(webSearchCall("c1", `{"query":"golang generics","count":3}`), providertest.Response{Text: "done"})
	defer srv.Close()
	m := webSearchModel(t, srv)
	m, _ = m.Command("look it up")
	m = waitPrompt(t, m)
	v := m.View()
	for _, want := range []string{"[main] web_search", "golang generics", "y / n / a"} {
		if !strings.Contains(v, want) {
			t.Errorf("view lacks %q:\n%s", want, v)
		}
	}
	if p := strings.Join(m.approvalLines(), "\n"); strings.Contains(p, `"count"`) {
		t.Errorf("prompt shows raw args:\n%s", p)
	}
}

func TestApprovalWebSearchSecret(t *testing.T) {
	srv := providertest.New(webSearchCall("c1", `{"query":"leak SECRET123 now"}`), providertest.Response{Text: "done"})
	defer srv.Close()
	m := webSearchModel(t, srv)
	m, _ = m.Command("look it up")
	m = waitPrompt(t, m)
	v := m.View()
	p := strings.Join(m.approvalLines(), "\n")
	if strings.Contains(v, "SECRET123") {
		t.Errorf("secret in view:\n%s", v)
	}
	if !strings.Contains(p, "[redacted]") {
		t.Errorf("no [redacted] in prompt:\n%s", p)
	}
}
