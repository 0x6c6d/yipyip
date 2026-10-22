package tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"yipyip/internal/provider/providertest"
)

// skillModel builds a model whose start directory holds the project skill
// greet with the body "Hello there.".
func skillModel(t *testing.T, srv *providertest.Server) Model {
	t.Helper()
	m, _ := newSessionModelOpts(t, srv, "start", func(o *Options) {
		p := filepath.Join(o.StartDir, ".yipyip", "skills", "greet", "SKILL.md")
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte("---\nname: greet\ndescription: Say hi\n---\n\nHello there.\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	})
	return m
}

// lastUserMessage is the last user message of the last request.
func lastUserMessage(t *testing.T, srv *providertest.Server) string {
	t.Helper()
	reqs := srv.Requests()
	if len(reqs) == 0 {
		t.Fatal("no request sent")
	}
	last := ""
	for _, msg := range reqs[len(reqs)-1].Messages {
		if msg.Role == "user" {
			last = msg.Content
		}
	}
	return last
}

func TestSkillRun(t *testing.T) {
	srv := providertest.New(providertest.Response{Text: "ok"})
	defer srv.Close()
	m := skillModel(t, srv)
	m, _ = m.Command("/greet Bob")
	waitSession(t, m.sess)
	m = m.Drain()
	if got := lastUserMessage(t, srv); got != "Hello there.\n\nBob" {
		t.Fatalf("user message = %q", got)
	}
	if v := ansi.Strip(m.View()); !strings.Contains(v, "/greet Bob") {
		t.Fatalf("typed line not in view:\n%s", v)
	}
}

func TestSkillRunNoArgs(t *testing.T) {
	srv := providertest.New(providertest.Response{Text: "ok"})
	defer srv.Close()
	m := skillModel(t, srv)
	m, _ = m.Command("/greet")
	waitSession(t, m.sess)
	_ = m.Drain()
	if got := lastUserMessage(t, srv); got != "Hello there." {
		t.Fatalf("user message = %q", got)
	}
}

func TestSkillUnknown(t *testing.T) {
	srv := providertest.New(providertest.Response{Text: "ok"})
	defer srv.Close()
	m := skillModel(t, srv)
	m, _ = m.Command("/nosuch")
	waitSession(t, m.sess)
	m = m.Drain()
	if n := len(srv.Requests()); n != 0 {
		t.Fatalf("%d requests sent for an unknown command", n)
	}
	if v := ansi.Strip(m.View()); !strings.Contains(v, "unknown command: /nosuch") {
		t.Fatalf("no unknown-command line:\n%s", v)
	}
}
