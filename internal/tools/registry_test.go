package tools

import (
	"context"
	"encoding/json"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"yipyip/internal/config"
)

func newTestRegistry(t *testing.T, opts Options) *Registry {
	t.Helper()
	if opts.StartDir == "" {
		opts.StartDir = t.TempDir()
	}
	r, err := NewRegistry(opts)
	if err != nil {
		t.Fatalf("NewRegistry: %v", err)
	}
	return r
}

func TestRegistryNoWebTools(t *testing.T) {
	r := newTestRegistry(t, Options{})
	var names []string
	for _, tool := range r.Tools() {
		names = append(names, tool.Name)
		if tool.Name == WebFetchTool {
			continue
		}
		l := strings.ToLower(tool.Name)
		for _, bad := range forbiddenToolWords {
			if strings.Contains(l, bad) {
				t.Errorf("web-like tool registered: %q", tool.Name)
			}
		}
	}
	if !sort.StringsAreSorted(names) {
		t.Errorf("Tools() not sorted: %v", names)
	}
	if !r.Has("bash") {
		t.Errorf("bash not registered: %v", names)
	}
	if !r.Has("web_fetch") {
		t.Errorf("web_fetch not registered: %v", names)
	}
	for _, n := range []string{"web_search", "fetch", "curl", "http_get", "download"} {
		if r.Has(n) {
			t.Errorf("Has(%q) = true", n)
		}
	}
	for _, tool := range r.Tools() {
		if tool.Name == "bash" && !tool.Gated {
			t.Errorf("bash must be gated")
		}
		if tool.Name == "web_fetch" && !tool.Gated {
			t.Errorf("web_fetch must be gated")
		}
	}

	for _, n := range []string{"fetch", "Web_Fetch"} {
		func() {
			defer func() {
				if recover() == nil {
					t.Errorf("registering web-like tool %q did not panic", n)
				}
			}()
			r.register(Tool{Name: n, Parameters: json.RawMessage(`{"type":"object"}`)},
				func(context.Context, json.RawMessage) Result { return Result{} })
		}()
		if r.Has(n) {
			t.Errorf("%q registered", n)
		}
	}
}

func TestRegistryBadStartDir(t *testing.T) {
	if _, err := NewRegistry(Options{StartDir: filepath.Join(t.TempDir(), "missing")}); err == nil {
		t.Errorf("NewRegistry with missing start dir: no error")
	}
	if _, err := NewRegistry(Options{}); err == nil {
		t.Errorf("NewRegistry with empty start dir: no error")
	}
}

func TestRegistryUnknownTool(t *testing.T) {
	r := newTestRegistry(t, Options{Redactor: config.NewRedactor("SECRET123")})
	res := r.Run(context.Background(), "no_such_tool", json.RawMessage(`{"url":"http://example.org"}`))
	if !res.IsError || res.Output != "unknown tool: no_such_tool" {
		t.Errorf("unknown tool result = %+v", res)
	}
	res = r.Run(context.Background(), "SECRET123", json.RawMessage(`{}`))
	if !res.IsError || strings.Contains(res.Output, "SECRET123") || !strings.Contains(res.Output, config.Redacted) {
		t.Errorf("unknown tool name not redacted: %+v", res)
	}
}

func TestRegistryInvalidArgs(t *testing.T) {
	r := newTestRegistry(t, Options{})
	ctx := context.Background()
	for _, a := range []string{``, `null`, `{not json`, `[]`, `"echo hi"`, `42`, `{}`,
		`{"command": 5}`, `{"command": null}`, `{"command": ""}`,
		`{"command":"echo hi","timeout_seconds":"5"}`, `{"command":"echo hi","timeout_seconds":1.5}`} {
		res := r.Run(ctx, "bash", json.RawMessage(a))
		if !res.IsError {
			t.Errorf("bash accepted args %q: %+v", a, res)
		}
		if strings.Contains(res.Output, "[exit code:") {
			t.Errorf("bash ran with args %q: %+v", a, res)
		}
	}
	res := r.Run(ctx, "bash", json.RawMessage(`{not json`))
	if !strings.HasPrefix(res.Output, "invalid arguments") {
		t.Errorf("invalid JSON output = %q", res.Output)
	}
}

func TestRegistryPanicRecovered(t *testing.T) {
	r := newTestRegistry(t, Options{Redactor: config.NewRedactor("SECRET123")})
	r.register(Tool{Name: "boom", Parameters: json.RawMessage(`{"type":"object"}`)},
		func(context.Context, json.RawMessage) Result { panic("bad SECRET123") })
	res := r.Run(context.Background(), "boom", nil)
	if !res.IsError || strings.Contains(res.Output, "SECRET123") || !strings.Contains(res.Output, "panic") {
		t.Errorf("panic result = %+v", res)
	}
}

func TestBashToolOutput(t *testing.T) {
	r := newTestRegistry(t, Options{})
	ctx := context.Background()

	res := r.Run(ctx, "bash", json.RawMessage(`{"command":"echo hi; exit 4"}`))
	if !res.IsError || res.Output != "hi\n[exit code: 4]" {
		t.Errorf("exit 4 result = %+v", res)
	}
	res = r.Run(ctx, "bash", json.RawMessage(`{"command":"echo fine","timeout_seconds":5}`))
	if res.IsError || res.Output != "fine\n[exit code: 0]" {
		t.Errorf("exit 0 result = %+v", res)
	}
	res = r.Run(ctx, "bash", json.RawMessage(`{"command":"printf x"}`))
	if res.IsError || res.Output != "x\n[exit code: 0]" {
		t.Errorf("no trailing newline result = %+v", res)
	}
	res = r.Run(ctx, "bash", json.RawMessage(`{"command":"pwd"}`))
	if res.IsError || !strings.HasPrefix(res.Output, r.Guard().Root()+"\n") {
		t.Errorf("pwd result = %+v, want root %s", res, r.Guard().Root())
	}

	start := time.Now()
	res = r.Run(ctx, "bash", json.RawMessage(`{"command":"sleep 30","timeout_seconds":1}`))
	if !res.IsError || !strings.Contains(res.Output, "[exit code: -1] [timed out after 1s]") {
		t.Errorf("timeout result = %+v", res)
	}
	if time.Since(start) > 15*time.Second {
		t.Errorf("timeout took %v", time.Since(start))
	}

	cctx, cancel := context.WithTimeout(ctx, 300*time.Millisecond)
	defer cancel()
	res = r.Run(cctx, "bash", json.RawMessage(`{"command":"sleep 30"}`))
	if !res.IsError || !strings.Contains(res.Output, "[exit code: -1] [cancelled]") {
		t.Errorf("cancel result = %+v", res)
	}
}

func TestToolSchemasValidJSON(t *testing.T) {
	r := newTestRegistry(t, Options{})
	tools := r.Tools()
	if len(tools) == 0 {
		t.Fatal("no tools registered")
	}
	for _, tool := range tools {
		if tool.Name == "" || tool.Description == "" {
			t.Errorf("tool %q lacks name or description", tool.Name)
		}
		var schema map[string]any
		if err := json.Unmarshal(tool.Parameters, &schema); err != nil {
			t.Errorf("tool %s: schema is not valid JSON: %v", tool.Name, err)
			continue
		}
		if schema["type"] != "object" {
			t.Errorf("tool %s: schema type = %v, want object", tool.Name, schema["type"])
		}
		if _, ok := schema["properties"].(map[string]any); !ok {
			t.Errorf("tool %s: schema has no properties object", tool.Name)
		}
	}
}

func TestSecretToolResult(t *testing.T) {
	r := newTestRegistry(t, Options{Redactor: config.NewRedactor("SECRET123")})
	ctx := context.Background()
	res := r.Run(ctx, "bash", json.RawMessage(`{"command":"echo SECRET123"}`))
	if strings.Contains(res.Output, "SECRET123") || !strings.Contains(res.Output, config.Redacted) {
		t.Errorf("tool result not redacted: %q", res.Output)
	}
	res = r.Run(ctx, "bash", json.RawMessage(`{"command":"echo SECRET123 >&2; exit 2"}`))
	if !res.IsError || strings.Contains(res.Output, "SECRET123") || !strings.Contains(res.Output, config.Redacted) {
		t.Errorf("error result not redacted: %+v", res)
	}
	res = r.Run(ctx, "bash", json.RawMessage(`{"command":5,"x":"SECRET123"`))
	if strings.Contains(res.Output, "SECRET123") {
		t.Errorf("invalid-args result leaks secret: %q", res.Output)
	}
}

func TestBashToolStripEnv(t *testing.T) {
	t.Setenv("YIP_TEST_STRIP", "SECRET123")
	t.Setenv("YIP_TEST_KEEP", "kept")
	r := newTestRegistry(t, Options{StripEnv: []string{"YIP_TEST_STRIP"}})
	res := r.Run(context.Background(), "bash",
		json.RawMessage(`{"command":"echo \"s=${YIP_TEST_STRIP:-unset} k=${YIP_TEST_KEEP:-unset}\""}`))
	if res.IsError || !strings.Contains(res.Output, "s=unset k=kept") {
		t.Errorf("StripEnv not applied: %+v", res)
	}
}
