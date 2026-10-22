package e2e

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"yipyip/internal/provider/providertest"
)

func TestSkillE2E(t *testing.T) {
	e := newEnv(t,
		resp{Text: "review done"},
		resp{ToolCalls: []providertest.ToolCall{call("c1", "write_file", `{"path":"notes.txt","content":"hello"}`)}},
		resp{Text: "note done"},
	)

	skill := func(root, name, desc, body string) string {
		dir := filepath.Join(root, name)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		p := filepath.Join(dir, "SKILL.md")
		content := "---\nname: " + name + "\ndescription: " + desc + "\n---\n" + body + "\n"
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
		return p
	}
	gdir := filepath.Join(e.Home, ".config", "yipyip", "skills")
	pdir := filepath.Join(e.Dir, ".yipyip", "skills")
	files := []string{
		skill(gdir, "review", "global review", "GLOBAL REVIEW"),
		skill(pdir, "review", "project review", "Review the code. Key: SECRET123"),
		skill(gdir, "note", "write a note", "Write the file notes.txt with the text hello."),
	}
	before := map[string]string{}
	for _, f := range files {
		b, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		before[f] = string(b)
	}

	e.submit(t, "/skills")
	view := e.Model.View()
	mustContain(t, view, "/review", "(project)", "/note", "(global)")
	if n := strings.Count(view, "/review"); n != 1 {
		t.Errorf("/review listed %d times:\n%s", n, view)
	}

	e.submit(t, "/review focus on tests")
	reqs := e.Server.Requests()
	if len(reqs) != 1 {
		t.Fatalf("want 1 request after /review, got %d", len(reqs))
	}
	var last string
	for _, m := range reqs[0].Messages {
		if m.Role == "user" {
			last = m.Content
		}
	}
	if want := "Review the code. Key: [redacted]\n\nfocus on tests"; last != want {
		t.Errorf("user message = %q, want %q", last, want)
	}

	e.submit(t, "/note")
	if got := e.read("notes.txt"); got != "hello" {
		t.Errorf("notes.txt = %q, want written in auto mode", got)
	}

	for _, r := range e.Server.Requests() {
		if strings.Contains(string(r.Body), "SECRET123") {
			t.Errorf("request body contains SECRET123: %s", r.Body)
		}
		if strings.Contains(string(r.Body), "GLOBAL REVIEW") {
			t.Errorf("overridden global skill was sent: %s", r.Body)
		}
	}

	for _, f := range files {
		b, err := os.ReadFile(f)
		if err != nil || string(b) != before[f] {
			t.Errorf("SKILL.md %s changed (err %v)", f, err)
		}
	}

	e.submit(t, "/exit")
	sdir := filepath.Join(e.Dir, ".yipyip", "sessions")
	n := 0
	err := filepath.Walk(sdir, func(p string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return err
		}
		n++
		b, rerr := os.ReadFile(p)
		if rerr != nil {
			return rerr
		}
		if strings.Contains(string(b), "SECRET123") {
			t.Errorf("session file %s contains SECRET123", p)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if n == 0 {
		t.Errorf("no session file written under %s", sdir)
	}
}
