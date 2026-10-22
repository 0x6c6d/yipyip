package tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"yipyip/internal/agent"
	"yipyip/internal/config"
	"yipyip/internal/provider/providertest"
)

// newJevModel builds a model whose config has the given "jev" value in the
// project file (none when jev is ""); TYPESAFE_API_KEY resolves to a fake key.
func newJevModel(t *testing.T, srv *providertest.Server, jev string) (Model, *agent.Session) {
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
	if err := os.WriteFile(config.GlobalPath(home), []byte(providertest.ConfigJSON(srv.URL, "{env:FAKE_KEY}", 8000)), 0o644); err != nil {
		t.Fatal(err)
	}
	if jev != "" {
		if err := os.WriteFile(filepath.Join(dir, config.ProjectFile), []byte(`{"jev":`+jev+`}`), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	cfg, err := config.Load(home, dir, func(k string) string {
		switch k {
		case "FAKE_KEY":
			return "SECRET123"
		case "TYPESAFE_API_KEY":
			return "jev-key"
		}
		return ""
	})
	if err != nil {
		t.Fatal(err)
	}
	newSess := func() (*agent.Session, error) {
		s, err := agent.NewSession(agent.Options{Config: cfg, StartDir: dir})
		if err == nil {
			t.Cleanup(func() { s.Cancel(); waitSession(t, s) })
		}
		return s, err
	}
	sess, err := newSess()
	if err != nil {
		t.Fatal(err)
	}
	m := New(Options{StartDir: dir, Session: sess, NewSession: newSess})
	next, _ := m.Update(tea.WindowSizeMsg{Width: 160, Height: 40})
	return next.(Model), sess
}

func jevJSON(url string) string {
	return `{"baseURL":"` + url + `","apiKey":"{env:TYPESAFE_API_KEY}"}`
}

func jevStatusRow(m Model) string {
	row1, _ := m.StatusRows()
	return row1
}

func TestJevToggle(t *testing.T) {
	srv := providertest.New()
	defer srv.Close()
	jev := providertest.NewJev(func(int, providertest.JevRequest) providertest.JevResponse {
		return providertest.JevResponse{}
	})
	defer jev.Close()
	m, sess := newJevModel(t, srv, jevJSON(jev.URL))
	if strings.Contains(plain(m), "jev on") || strings.Contains(jevStatusRow(m), "jev") {
		t.Fatalf("jev shown at start:\n%s", plain(m))
	}
	m, _ = m.Command("/jev")
	if !sess.JevOn() || !strings.Contains(plain(m), "jev on") || !strings.Contains(jevStatusRow(m), "jev") {
		t.Fatalf("after /jev:\n%s", plain(m))
	}
	m, _ = m.Command("/new")
	if !m.sess.JevOn() || !strings.Contains(jevStatusRow(m), "jev") {
		t.Fatalf("second tab does not follow: on=%v", m.sess.JevOn())
	}
	m, _ = m.Command("/jev")
	if m.sess.JevOn() || sess.JevOn() || !strings.Contains(plain(m), "jev off") || strings.Contains(jevStatusRow(m), "jev") {
		t.Fatalf("after second /jev:\n%s", plain(m))
	}
}

func TestJevUnconfigured(t *testing.T) {
	srv := providertest.New()
	defer srv.Close()
	for name, jev := range map[string]string{
		"no key":         "",
		"jev, no apiKey": `{"baseURL":"http://127.0.0.1:1"}`,
	} {
		m, sess := newJevModel(t, srv, jev)
		m, _ = m.Command("/jev")
		if !strings.Contains(plain(m), "jev is not configured") || sess.JevOn() || strings.Contains(jevStatusRow(m), "jev") {
			t.Fatalf("%s:\n%s", name, plain(m))
		}
	}
}

func TestJevStats(t *testing.T) {
	srv := providertest.New(questionCall("Which color?", []string{"alpha", "beta"}, false), providertest.Response{Text: "thanks"})
	defer srv.Close()
	jev := providertest.NewJev(func(_ int, req providertest.JevRequest) providertest.JevResponse {
		answers := map[string]any{}
		for name, q := range req.Questions {
			if qm, _ := q.(map[string]any); qm["type"] == "choice" {
				answers[name] = providertest.JevChoiceAnswer("beta", 0.9)
			} else {
				answers[name] = providertest.JevScoreAnswer(0.5, 0.9)
			}
		}
		return providertest.JevResponse{Answers: answers}
	})
	defer jev.Close()
	m, sess := newJevModel(t, srv, jevJSON(jev.URL))
	m, _ = m.Command("/jev")
	m, _ = m.Command("ask me")
	waitSession(t, sess)
	m = m.Drain()
	m, _ = m.Command("/jev stats")
	v := plain(m)
	for _, want := range []string{"jev answered: 1", "jev fallbacks: 0", "jev input tokens: ",
		"main tokens question: ", "main tokens routing: ", "main tokens other: "} {
		if !strings.Contains(v, want) {
			t.Errorf("missing %q in:\n%s", want, v)
		}
	}
	m, _ = m.Command("/jev bogus")
	if !strings.Contains(plain(m), "error: usage: /jev [stats]") {
		t.Errorf("no usage error:\n%s", plain(m))
	}
}

func TestJevHelp(t *testing.T) {
	srv := providertest.New()
	defer srv.Close()
	m, _ := newJevModel(t, srv, "")
	m, _ = m.Command("/help")
	v := plain(m)
	if !strings.Contains(v, "/jev ") || !strings.Contains(v, "/jev stats") {
		t.Fatalf("/help does not list /jev:\n%s", v)
	}
}
