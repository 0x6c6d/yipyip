package e2e

import (
	"strings"
	"testing"

	"yipyip/internal/provider/providertest"
)

const agentsContent = "# AGENTS\n\nBuild: make build\nTest: make check\n"

func TestInitCreatesAgentsMD(t *testing.T) {
	e := newEnv(t,
		resp{ToolCalls: []providertest.ToolCall{
			call("c1", "write_file", `{"path":"AGENTS.md","content":"# AGENTS\n\nBuild: make build\nTest: make check\n"}`),
		}},
		resp{Text: "AGENTS.md written"})
	e.submit(t, "/init")
	if got := e.read("AGENTS.md"); got != agentsContent {
		t.Fatalf("AGENTS.md = %q, want %q", got, agentsContent)
	}
	mustContain(t, e.Model.View(), "initializing AGENTS.md ...", "AGENTS.md written")
	reqs := e.Server.Requests()
	if len(reqs) != 2 {
		t.Fatalf("%d requests, want 2", len(reqs))
	}
	var user string
	for _, m := range reqs[0].Messages {
		if m.Role == "user" {
			user = m.Content
		}
	}
	if !strings.Contains(user, "AGENTS.md") || !strings.Contains(user, "write_file") {
		t.Fatalf("init request does not ask for AGENTS.md via write_file: %q", user)
	}
	if strings.Contains(user, "BEGIN CURRENT") {
		t.Fatalf("init request embeds an existing file although none exists: %q", user)
	}
}

func TestInitUpdatesExisting(t *testing.T) {
	e := newEnv(t, resp{Text: "nothing to change"})
	e.write(t, "AGENTS.md", "# Notes\nKEEP-ME\n")
	e.submit(t, "/init")
	reqs := e.Server.Requests()
	if len(reqs) != 1 {
		t.Fatalf("%d requests, want 1", len(reqs))
	}
	body := string(reqs[0].Body)
	for _, want := range []string{"KEEP-ME", "AGENTS.md"} {
		if !strings.Contains(body, want) {
			t.Errorf("request does not contain %q:\n%s", want, body)
		}
	}
	var last providertest.Message
	for _, m := range reqs[0].Messages {
		last = m
	}
	if last.Role != "user" || !strings.Contains(last.Content, "KEEP-ME") {
		t.Fatalf("last message = %+v, want the init user message with KEEP-ME", last)
	}
}

func TestInitNoModel(t *testing.T) {
	srv := providertest.New(resp{Text: "should not be asked"})
	e := newEnvServer(t, srv, envOpts{noModel: true})
	e.submit(t, "/init")
	mustContain(t, e.Model.View(), "no model configured")
	mustNotContain(t, e.Model.View(), "initializing")
	if n := len(srv.Requests()); n != 0 {
		t.Fatalf("%d requests sent to the fake, want 0", n)
	}
	if got := e.read("AGENTS.md"); got != "<missing>" {
		t.Fatalf("AGENTS.md exists: %q", got)
	}
}
