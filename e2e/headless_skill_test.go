package e2e

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"yipyip/internal/provider/providertest"
)

func writeSkill(t *testing.T, dir, name, body string) {
	t.Helper()
	d := filepath.Join(dir, ".yipyip", "skills", name)
	if err := os.MkdirAll(d, 0o755); err != nil {
		t.Fatal(err)
	}
	src := "---\nname: " + name + "\ndescription: d\n---\n" + body + "\n"
	if err := os.WriteFile(filepath.Join(d, "SKILL.md"), []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
}

func checkSlashRefused(t *testing.T, srv *providertest.Server, res cliResult, word string) {
	t.Helper()
	if res.code != 2 || res.stdout != "" {
		t.Fatalf("code %d stdout %q stderr %q", res.code, res.stdout, res.stderr)
	}
	lines := strings.Split(strings.TrimRight(res.stderr, "\n"), "\n")
	if len(lines) != 1 || !strings.Contains(lines[0], word) {
		t.Errorf("stderr %q", res.stderr)
	}
	if n := len(srv.Requests()); n != 0 {
		t.Errorf("%d requests, want 0", n)
	}
	checkNoSecret(t, res)
}

func TestHeadlessSkill(t *testing.T) {
	srv := providertest.New(providertest.Response{Text: "ok"})
	defer srv.Close()
	home, dir := headlessDirs(t, srv)
	writeSkill(t, dir, "greet", "SKILL-BODY-7")
	res := runYip(t, home, dir, "", headlessEnv, "-p", "/greet world")
	if res.code != 0 {
		t.Fatalf("code %d stdout %q stderr %q", res.code, res.stdout, res.stderr)
	}
	reqs := srv.Requests()
	if len(reqs) == 0 {
		t.Fatal("no provider request")
	}
	msgs := reqs[len(reqs)-1].Messages
	var last string
	for _, m := range msgs {
		if m.Role == "user" {
			last = m.Content
		}
	}
	if last != "SKILL-BODY-7\n\nworld" {
		t.Errorf("user message %q", last)
	}
	checkNoSecret(t, res)
}

func TestHeadlessSlashModel(t *testing.T) {
	srv := providertest.New(providertest.Response{Text: "ok"})
	defer srv.Close()
	home, dir := headlessDirs(t, srv)
	checkSlashRefused(t, srv, runYip(t, home, dir, "", headlessEnv, "-p", "/model"), "/model")
}

func TestHeadlessSlashUnknown(t *testing.T) {
	srv := providertest.New(providertest.Response{Text: "ok"})
	defer srv.Close()
	home, dir := headlessDirs(t, srv)
	checkSlashRefused(t, srv, runYip(t, home, dir, "", headlessEnv, "-p", "/nosuch"), "/nosuch")
}

func TestHeadlessSkillShadowed(t *testing.T) {
	srv := providertest.New(providertest.Response{Text: "ok"})
	defer srv.Close()
	home, dir := headlessDirs(t, srv)
	writeSkill(t, dir, "help", "HELP-BODY")
	checkSlashRefused(t, srv, runYip(t, home, dir, "", headlessEnv, "-p", "/help"), "/help")
}
