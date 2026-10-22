package tui

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"yipyip/internal/agent"
	"yipyip/internal/provider/providertest"
	"yipyip/internal/session"
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

// writeSkill writes root/name/SKILL.md with the given content.
func writeSkill(t *testing.T, root, name, content string) {
	t.Helper()
	p := filepath.Join(root, name, "SKILL.md")
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// skillsModel builds a session model with the given project and global
// skills (name -> SKILL.md content) and a temp home; saved collects the
// session data passed to SaveOnExit.
func skillsModel(t *testing.T, srv *providertest.Server, project, global map[string]string, saved *[]*session.SessionData) (Model, *agent.Session) {
	t.Helper()
	return newSessionModelOpts(t, srv, "start", func(o *Options) {
		home := filepath.Join(filepath.Dir(o.StartDir), "skillhome")
		for name, c := range project {
			writeSkill(t, filepath.Join(o.StartDir, ".yipyip", "skills"), name, c)
		}
		for name, c := range global {
			writeSkill(t, filepath.Join(home, ".config", "yipyip", "skills"), name, c)
		}
		o.Home = home
		o.SaveOnExit = func(d *session.SessionData) error {
			if saved != nil {
				*saved = append(*saved, d)
			}
			return nil
		}
	})
}

// requestText joins the contents of all messages of all requests.
func requestText(srv *providertest.Server) string {
	var b strings.Builder
	for _, q := range srv.Requests() {
		for _, msg := range q.Messages {
			b.WriteString(msg.Content + "\n")
		}
	}
	return b.String()
}

func TestSkillBuiltinWins(t *testing.T) {
	srv := providertest.New(providertest.Response{Text: "ok"})
	defer srv.Close()
	m, sess := skillsModel(t, srv, map[string]string{"help": "---\n---\nSKILLBODY\n"}, nil, nil)
	m, _ = m.Command("/help")
	waitSession(t, sess)
	m = m.Drain()
	if n := len(srv.Requests()); n != 0 {
		t.Fatalf("%d requests sent for /help", n)
	}
	v := ansi.Strip(m.View())
	if strings.Contains(v, "SKILLBODY") || !strings.Contains(v, "/sessions") {
		t.Fatalf("help text not shown or skill body shown:\n%s", v)
	}
}

func TestSkillSecretRedacted(t *testing.T) {
	srv := providertest.New(providertest.Response{Text: "ok"})
	defer srv.Close()
	var saved []*session.SessionData
	m, sess := skillsModel(t, srv, map[string]string{"leak": "---\ndescription: d\n---\nUse key SECRET123 now.\n"}, nil, &saved)
	m, _ = m.Command("/leak with SECRET123 too")
	waitSession(t, sess)
	m = m.Drain()
	sent := requestText(srv)
	if strings.Contains(sent, "SECRET123") || !strings.Contains(sent, "[redacted]") {
		t.Fatalf("model got %q", sent)
	}
	if v := ansi.Strip(m.View()); strings.Contains(v, "SECRET123") {
		t.Fatalf("secret in view:\n%s", v)
	}
	m, _ = m.Command("/exit")
	if len(saved) == 0 {
		t.Fatal("nothing saved on /exit")
	}
	for _, d := range saved {
		b, err := json.Marshal(d)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(b), "SECRET123") {
			t.Fatalf("secret in saved session: %s", b)
		}
	}
	if v := ansi.Strip(m.View()); strings.Contains(v, "SECRET123") {
		t.Fatalf("secret in view after exit:\n%s", v)
	}
}

func TestSkillWarnings(t *testing.T) {
	srv := providertest.New(providertest.Response{Text: "ok"})
	defer srv.Close()
	m, sess := skillsModel(t, srv, map[string]string{
		"Bad_Name": "---\n---\nbad\n",
		"good":     "---\n---\nGood body.\n",
	}, nil, nil)
	v := ansi.Strip(m.View())
	if strings.Count(v, "warning:") != 1 || !strings.Contains(v, "Bad_Name") {
		t.Fatalf("want one warning naming Bad_Name:\n%s", v)
	}
	m, _ = m.Command("/good")
	waitSession(t, sess)
	_ = m.Drain()
	if got := lastUserMessage(t, srv); got != "Good body." {
		t.Fatalf("user message = %q", got)
	}
}

func TestSkillArgsAppended(t *testing.T) {
	srv := providertest.New(providertest.Response{Text: "ok"})
	defer srv.Close()
	m, sess := skillsModel(t, srv, nil, map[string]string{"hello": "---\n---\nHi from global.\n"}, nil)
	m, _ = m.Command("/hello x y")
	waitSession(t, sess)
	_ = m.Drain()
	if got := lastUserMessage(t, srv); got != "Hi from global.\n\nx y" {
		t.Fatalf("user message = %q", got)
	}
}
