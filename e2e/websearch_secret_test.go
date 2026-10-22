package e2e

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"yipyip/internal/provider/providertest"
)

const braveKey = "BRAVEKEY-E2E"

// searchJSON is a project yipyip.json with a search block pointing at the
// fake Brave endpoint, its key taken from BRAVE_API_KEY.
func searchJSON(braveURL string) string {
	return `{"search":{"baseURL":"` + braveURL + `","apiKey":"{env:BRAVE_API_KEY}"}}`
}

// scriptedTool plays one tool call for the user's message and a text reply
// once the tool result is back.
func scriptedTool(name, args, final string) *providertest.Server {
	return providertest.NewFunc(func(_ int, req providertest.Request) resp {
		last := req.Messages[len(req.Messages)-1]
		if last.Role == "tool" {
			return resp{Text: final}
		}
		return resp{ToolCalls: []providertest.ToolCall{call("c1", name, args)}}
	})
}

func TestSecretWebSearchE2E(t *testing.T) {
	b := providertest.NewBrave(func(int, providertest.BraveRequest) providertest.BraveResponse {
		return providertest.BraveResponse{Results: []providertest.BraveResult{{
			Title:       "title SECRET123 BRAVEKEY-E2E",
			URL:         "https://result.example/SECRET123/BRAVEKEY-E2E",
			Description: "snippet SECRET123 and BRAVEKEY-E2E",
		}}}
	})
	t.Cleanup(b.Close)
	srv := scriptedTool("web_search", `{"query":"look up SECRET123 please"}`, "search finished")
	e := newEnvServer(t, srv, envOpts{contextLimit: 8000, projectJSON: searchJSON(b.URL)})

	e.submit(t, "search the web")

	reqs := b.Requests()
	if len(reqs) != 1 {
		t.Fatalf("Brave requests = %d, want 1", len(reqs))
	}
	q := reqs[0].Query.Get("q")
	if !strings.Contains(q, "[redacted]") || strings.Contains(q, secret) {
		t.Errorf("Brave query q = %q", q)
	}
	if got := reqs[0].Header.Get("X-Subscription-Token"); got != braveKey {
		t.Errorf("X-Subscription-Token = %q, want the Brave key", got)
	}
	mustNotContain(t, reqs[0].RawQuery, secret, braveKey)

	v := e.Model.View()
	mustContain(t, v, "[redacted]", "search finished")
	mustNotContain(t, v, secret, braveKey)
	mustNotContain(t, e.Model.StatusLine(), secret, braveKey)

	preqs := e.Server.Requests()
	if len(preqs) < 2 {
		t.Fatalf("provider requests = %d, want >= 2", len(preqs))
	}
	for _, r := range preqs {
		mustNotContain(t, string(r.Body), secret, braveKey)
	}
	// The redacted result reached the model.
	mustContain(t, string(preqs[len(preqs)-1].Body), "[redacted]")

	for _, m := range e.Session.History() {
		mustNotContain(t, m.Content, secret, braveKey)
		for _, tc := range m.ToolCalls {
			mustNotContain(t, tc.Arguments, secret, braveKey)
		}
	}

	filesUnder(t, e.Home)
	filesUnder(t, e.Dir)
	for _, root := range []string{e.Home, e.Dir} {
		noFileContains(t, root, braveKey)
	}
}

func TestSecretBraveKeyNotInBashEnv(t *testing.T) {
	// The real environment holds the Brave key too: bash must not inherit it.
	t.Setenv("BRAVE_API_KEY", braveKey)
	b := providertest.NewBrave(func(int, providertest.BraveRequest) providertest.BraveResponse {
		return providertest.BraveResponse{}
	})
	t.Cleanup(b.Close)
	srv := scriptedTool("bash", `{"command":"echo \"k=$BRAVE_API_KEY\""}`, "bash finished")
	e := newEnvServer(t, srv, envOpts{contextLimit: 8000, projectJSON: searchJSON(b.URL)})

	e.submit(t, "show the key")

	var result string
	for _, m := range e.Session.History() {
		if m.Role == "tool" {
			result = m.Content
		}
	}
	if !strings.Contains(result, "k=") {
		t.Fatalf("bash tool result = %q, want k=", result)
	}
	for _, line := range strings.Split(result, "\n") {
		if strings.HasPrefix(line, "k=") && line != "k=" {
			t.Errorf("bash saw BRAVE_API_KEY: %q", line)
		}
	}

	v := e.Model.View()
	mustContain(t, v, "k=", "bash finished")
	mustNotContain(t, v, braveKey, "[redacted]")
	for _, r := range e.Server.Requests() {
		mustNotContain(t, string(r.Body), braveKey)
	}
	if n := len(b.Requests()); n != 0 {
		t.Errorf("Brave requests = %d, want 0", n)
	}
	noFileContains(t, e.Home, braveKey)
	noFileContains(t, e.Dir, braveKey)
}

// noFileContains fails the test for any regular file under root holding s.
func noFileContains(t *testing.T, root, s string) {
	t.Helper()
	for _, rel := range filesUnder(t, root) {
		b, err := os.ReadFile(filepath.Join(root, rel))
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(b), s) {
			t.Errorf("file %s under %s contains %q", rel, root, s)
		}
	}
}
