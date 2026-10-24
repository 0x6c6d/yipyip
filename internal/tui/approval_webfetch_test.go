package tui

import (
	"strings"
	"testing"

	"yipyip/internal/provider/providertest"
)

func webFetchCall(id, args string) providertest.Response {
	return providertest.Response{ToolCalls: []providertest.ToolCall{
		{ID: id, Name: "web_fetch", Arguments: args},
	}}
}

func TestApprovalWebFetchURL(t *testing.T) {
	if got := argsSummary("web_fetch", `{"url":"http://127.0.0.1:9/doc"}`); got != "http://127.0.0.1:9/doc" {
		t.Fatalf("summary %q", got)
	}
	if got := argsSummary("web_fetch", `{}`); got != `{}` {
		t.Fatalf("fallback %q", got)
	}
	srv := providertest.New(webFetchCall("c1", `{"url":"http://127.0.0.1:9/doc"}`), providertest.Response{Text: "done"})
	defer srv.Close()
	m := webSearchModel(t, srv)
	m, _ = m.Command("fetch it")
	m = waitPrompt(t, m)
	v := m.View()
	for _, want := range []string{"[main] web_fetch", "http://127.0.0.1:9/doc", "y / n / a"} {
		if !strings.Contains(v, want) {
			t.Errorf("view lacks %q:\n%s", want, v)
		}
	}
	if p := strings.Join(m.approvalLines(), "\n"); strings.Contains(p, `"url"`) {
		t.Errorf("prompt shows raw args:\n%s", p)
	}
	_ = keyRunes(m, "n").Drain()
}

func TestApprovalWebFetchSecret(t *testing.T) {
	srv := providertest.New(webFetchCall("c1", `{"url":"http://127.0.0.1:9/doc?key=SECRET123"}`), providertest.Response{Text: "done"})
	defer srv.Close()
	m := webSearchModel(t, srv)
	m, _ = m.Command("fetch it")
	m = waitPrompt(t, m)
	v := m.View()
	p := strings.Join(m.approvalLines(), "\n")
	if strings.Contains(v, "SECRET123") {
		t.Errorf("secret in view:\n%s", v)
	}
	if !strings.Contains(p, "[redacted]") {
		t.Errorf("no [redacted] in prompt:\n%s", p)
	}
	if v := keyRunes(m, "n").Drain().View(); strings.Contains(v, "SECRET123") {
		t.Errorf("secret in view after deny:\n%s", v)
	}
}
