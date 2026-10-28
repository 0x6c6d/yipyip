package e2e

import (
	"encoding/json"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"yipyip/internal/config"
	"yipyip/internal/provider/providertest"
)

const secret = "SECRET123"

// secretServer echoes the request's Authorization header (the API key) in
// every reply it can:
//   - subagent: a text reply with the header;
//   - main, turn "go": text plus bash (env variable into a file and the
//     literal key), a subagent spawn and a wait; after the tool results an
//     HTTP 401 whose body contains the header;
//   - main, turn "write notes": write_file of notes.txt, then a text reply
//     with the header.
func secretServer() *providertest.Server {
	return providertest.NewFunc(func(_ int, req providertest.Request) resp {
		auth := req.Header.Get("Authorization")
		msgs := req.Messages
		if len(msgs) < 2 {
			return resp{Text: "reply " + auth}
		}
		if strings.Contains(msgs[0].Content, "You are subagent") {
			return resp{Text: "sub sees " + auth}
		}
		lastUser := ""
		for _, m := range msgs {
			if m.Role == "user" {
				lastUser = m.Content
			}
		}
		last := msgs[len(msgs)-1]
		switch {
		case last.Role == "tool" && strings.HasPrefix(lastUser, "go"):
			return resp{Status: 401, Body: `{"error":"bad header ` + auth + `"}`}
		case last.Role == "tool":
			return resp{Text: "done " + auth}
		case strings.HasPrefix(lastUser, "go"):
			return resp{Text: "auth=" + auth, ToolCalls: []providertest.ToolCall{
				call("c1", "bash", `{"command":"echo $FAKE_KEY > env.txt; echo SECRET123"}`),
				call("c2", "spawn_agent", `{"task":"echo the header"}`),
				call("c3", "wait_agent", `{"id":"a1"}`),
			}}
		case strings.HasPrefix(lastUser, "write notes"):
			return resp{ToolCalls: []providertest.ToolCall{
				call("c4", "write_file", `{"path":"notes.txt","content":"meeting at noon\n"}`),
			}}
		}
		return resp{Text: "reply " + auth}
	})
}

// filesUnder returns the regular files under root (relative paths) and fails
// the test for any file containing the secret.
func filesUnder(t *testing.T, root string) []string {
	t.Helper()
	var files []string
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		rel, _ := filepath.Rel(root, p)
		files = append(files, rel)
		b, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		if strings.Contains(string(b), secret) {
			t.Errorf("file %s contains the secret", p)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return files
}

func TestSecretEndToEnd(t *testing.T) {
	// The real environment holds the key too: bash must not inherit it.
	t.Setenv("FAKE_KEY", secret)
	e := newEnvServer(t, secretServer(), envOpts{contextLimit: 8000})

	e.submit(t, "go, the key is SECRET123")
	v := e.Model.View()
	mustContain(t, v, "auth=Bearer [redacted]", "401")
	mustNotContain(t, v, secret)

	e.submit(t, "write notes")
	if got := e.read("notes.txt"); got != "meeting at noon\n" {
		t.Fatalf("notes.txt = %q", got)
	}
	mustContain(t, e.Model.View(), "done Bearer [redacted]")
	e.submit(t, "/undo")
	if got := e.read("notes.txt"); got != "<missing>" {
		t.Fatalf("after /undo notes.txt = %q", got)
	}

	v = e.Model.View()
	mustContain(t, v, "undone:", "deleted notes.txt")
	mustNotContain(t, v, secret)
	mustNotContain(t, e.Model.StatusLine(), secret)

	// The scenario really sent the key and echoed it.
	reqs := e.Server.Requests()
	if len(reqs) < 4 || reqs[0].Header.Get("Authorization") != "Bearer "+secret {
		t.Fatalf("requests: %d", len(reqs))
	}
	for _, r := range reqs {
		mustNotContain(t, string(r.Body), secret)
	}
	for _, m := range e.Session.History() {
		mustNotContain(t, m.Content, secret)
		for _, tc := range m.ToolCalls {
			mustNotContain(t, tc.Arguments, secret)
		}
	}
	for _, sub := range e.Session.Subagents() {
		mustNotContain(t, sub.Task+sub.Final+sub.Err, secret)
		lines, _ := e.Session.SubagentTranscript(sub.ID)
		mustNotContain(t, strings.Join(lines, "\n"), secret)
	}

	// yipyip wrote no logs or history; the only session files are the
	// autosaves of the conversation, and no file holds the key.
	if got := filesUnder(t, e.Home); len(got) != 1 || got[0] != filepath.Join(".config", "yipyip", "config.json") {
		t.Errorf("files in HOME = %q", got)
	}
	var other []string
	for _, f := range filesUnder(t, e.Dir) {
		if filepath.Dir(f) == filepath.Join(".yipyip", "sessions") && strings.HasSuffix(f, ".json") {
			mustNotContain(t, e.read(f), secret)
			continue
		}
		other = append(other, f)
	}
	if len(other) != 1 || other[0] != "env.txt" {
		t.Errorf("files in the start directory = %q", other)
	}
	if got := strings.TrimSpace(e.read("env.txt")); got != "" {
		t.Errorf("bash saw the key variable: env.txt = %q", got)
	}
}

func TestSecretMissingEnvError(t *testing.T) {
	base := t.TempDir()
	home, dir := filepath.Join(base, "home"), filepath.Join(base, "start")
	for _, d := range []string{filepath.Join(home, ".config", "yipyip"), dir} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	cfg := map[string]any{"provider": map[string]any{"p": map[string]any{
		"name":    "{env:FAKE_KEY}",
		"options": map[string]any{"baseURL": "http://127.0.0.1:9/v1", "apiKey": "{env:YIP_TEST_UNSET}"},
		"models":  map[string]any{"m": map[string]any{}},
	}}}
	b, _ := json.Marshal(cfg)
	if err := os.WriteFile(config.GlobalPath(home), b, 0o644); err != nil {
		t.Fatal(err)
	}
	asked := map[string]bool{}
	_, err := config.Load(home, dir, func(k string) string {
		asked[k] = true
		if k == "FAKE_KEY" {
			return secret
		}
		return ""
	})
	if err == nil {
		t.Fatal("Load accepted an unset variable")
	}
	if !asked["FAKE_KEY"] {
		t.Error("FAKE_KEY was not resolved before the error")
	}
	if !strings.Contains(err.Error(), "YIP_TEST_UNSET") || strings.Contains(err.Error(), secret) {
		t.Errorf("Load error = %q", err)
	}
	if got := filesUnder(t, home); len(got) != 1 {
		t.Errorf("files in HOME = %q", got)
	}
}
