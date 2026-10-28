package tools

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"sort"
	"strings"
	"time"

	"yipyip/internal/config"
	"yipyip/internal/crashlog"
	"yipyip/internal/provider"
)

// Result is the outcome of one tool call, as reported to the model.
type Result struct {
	Output  string
	IsError bool
}

// Tool describes a tool offered to the model.
type Tool struct {
	Name, Description string
	Parameters        json.RawMessage // JSON schema, {"type":"object",...}
	Gated             bool            // needs approval in manual mode
}

// Options configures a Registry.
type Options struct {
	StartDir    string
	Redactor    *config.Redactor
	StripEnv    []string             // passed to BashRunner
	BeforeWrite func(absPath string) // called by write tools before modifying a file (may be nil)
	// Search is the web search client; nil = no web_search tool.
	Search *provider.SearchClient
	Fetch  *http.Client // web_fetch client; nil = provider.NewFetchHTTPClient(provider.FetchOptions{})
}

// toolFunc runs a tool with arguments that are known to be a JSON object.
type toolFunc func(ctx context.Context, args json.RawMessage) Result

type entry struct {
	tool       Tool
	run        toolFunc
	noRequired bool // empty/null arguments are accepted as {}
}

// Registry holds the built-in tools, confined to the start directory.
type Registry struct {
	opts  Options
	guard *PathGuard
	fetch *http.Client // web_fetch client (Options.Fetch or the default fetch client)
	tools map[string]entry
}

// NewRegistry returns a registry with all built-in tools. It fails if the
// start directory is invalid.
func NewRegistry(opts Options) (*Registry, error) {
	guard, err := NewPathGuard(opts.StartDir)
	if err != nil {
		return nil, err
	}
	r := &Registry{opts: opts, guard: guard, tools: make(map[string]entry)}

	// Built-in tools. Later tasks append their registration calls here
	// (each tool lives in its own file and calls r.register).
	r.registerBash()
	r.registerFileTools()
	r.registerSearchTools()
	if opts.Search != nil {
		r.registerWebSearch()
	}
	r.registerWebFetch()

	return r, nil
}

// forbiddenToolWords may never appear in a tool name: the only web tools are
// web_search (WebSearchTool) and web_fetch (WebFetchTool), which are exempt
// by their exact names.
var forbiddenToolWords = []string{"web", "fetch", "search", "browse", "http", "url", "download", "curl"}

// webToolName reports whether name is one of the two tool names exempt from
// the web-like name check (exact, case-sensitive match).
func webToolName(name string) bool { return name == WebSearchTool || name == WebFetchTool }

// register adds a built-in tool. It panics on programming errors (duplicate
// name, web-like name, schema that is not a JSON object schema). The only
// web tools are web_search and web_fetch: exactly these two names
// (case-sensitive) skip the web-like name check, every other name with a
// forbidden word panics.
func (r *Registry) register(t Tool, fn toolFunc) {
	if !webToolName(t.Name) {
		lower := strings.ToLower(t.Name)
		for _, bad := range forbiddenToolWords {
			if strings.Contains(lower, bad) {
				panic(fmt.Sprintf("tools: web-like tool name %q is not allowed", t.Name))
			}
		}
	}
	if _, dup := r.tools[t.Name]; dup {
		panic(fmt.Sprintf("tools: duplicate tool %q", t.Name))
	}
	var schema struct {
		Type     string   `json:"type"`
		Required []string `json:"required"`
	}
	if err := json.Unmarshal(t.Parameters, &schema); err != nil || schema.Type != "object" {
		panic(fmt.Sprintf("tools: tool %q has no JSON object schema", t.Name))
	}
	r.tools[t.Name] = entry{tool: t, run: fn, noRequired: len(schema.Required) == 0}
}

// Tools returns the registered tools sorted by name.
func (r *Registry) Tools() []Tool {
	out := make([]Tool, 0, len(r.tools))
	for _, e := range r.tools {
		out = append(out, e.tool)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// Has reports whether a tool with this name is registered.
func (r *Registry) Has(name string) bool {
	_, ok := r.tools[name]
	return ok
}

// Guard returns the path guard of the start directory.
func (r *Registry) Guard() *PathGuard { return r.guard }

// Run executes the named tool. Every returned Output is redacted.
func (r *Registry) Run(ctx context.Context, name string, args json.RawMessage) (res Result) {
	defer func() {
		if p := recover(); p != nil {
			crashlog.Recovered("tool "+name, p)
			res = Result{Output: fmt.Sprintf("tool %s failed: panic: %v", name, p), IsError: true}
		}
		res.Output = r.opts.Redactor.Redact(res.Output)
	}()

	e, ok := r.tools[name]
	if !ok {
		return Result{Output: "unknown tool: " + name, IsError: true}
	}
	trimmed := bytes.TrimSpace(args)
	if len(trimmed) == 0 || bytes.Equal(trimmed, []byte("null")) {
		if !e.noRequired {
			return Result{Output: "invalid arguments: expected a JSON object, got none", IsError: true}
		}
		trimmed = []byte("{}")
	}
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(trimmed, &obj); err != nil || obj == nil {
		msg := "expected a JSON object"
		if err != nil {
			msg += ": " + err.Error()
		}
		return Result{Output: "invalid arguments: " + msg, IsError: true}
	}
	return e.run(ctx, json.RawMessage(trimmed))
}

// errResult builds an error Result.
func errResult(format string, a ...any) Result {
	return Result{Output: fmt.Sprintf(format, a...), IsError: true}
}

// stringArg extracts a required string argument.
func stringArg(obj map[string]json.RawMessage, key string) (string, error) {
	raw, ok := obj[key]
	if !ok {
		return "", fmt.Errorf("missing required argument %q", key)
	}
	var s string
	if err := json.Unmarshal(raw, &s); err != nil || bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		return "", fmt.Errorf("argument %q must be a string", key)
	}
	return s, nil
}

// optIntArg extracts an optional integer argument; ok is false when absent or null.
func optIntArg(obj map[string]json.RawMessage, key string) (v int64, ok bool, err error) {
	raw, present := obj[key]
	if !present || bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		return 0, false, nil
	}
	var f float64
	if err := json.Unmarshal(raw, &f); err != nil {
		return 0, false, fmt.Errorf("argument %q must be an integer", key)
	}
	if f != math.Trunc(f) || math.IsInf(f, 0) || math.IsNaN(f) {
		return 0, false, fmt.Errorf("argument %q must be an integer", key)
	}
	if f > math.MaxInt32 {
		f = math.MaxInt32
	}
	if f < math.MinInt32 {
		f = math.MinInt32
	}
	return int64(f), true, nil
}

const bashSchema = `{"type":"object","properties":{"command":{"type":"string"},"timeout_seconds":{"type":"integer","minimum":1,"maximum":600}},"required":["command"]}`

func (r *Registry) registerBash() {
	r.register(Tool{
		Name: "bash",
		Description: "Run a shell command with bash -c in the start directory (stdin is /dev/null). " +
			"Returns combined stdout and stderr followed by the exit code. " +
			"timeout_seconds defaults to 120 and is capped at 600.",
		Parameters: json.RawMessage(bashSchema),
		Gated:      true,
	}, r.runBash)
}

func (r *Registry) runBash(ctx context.Context, args json.RawMessage) Result {
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(args, &obj); err != nil {
		return errResult("invalid arguments: %v", err)
	}
	command, err := stringArg(obj, "command")
	if err != nil {
		return errResult("invalid arguments: %v", err)
	}
	if strings.TrimSpace(command) == "" {
		return errResult("invalid arguments: argument %q is empty", "command")
	}
	secs, ok, err := optIntArg(obj, "timeout_seconds")
	if err != nil {
		return errResult("invalid arguments: %v", err)
	}
	timeout := DefaultTimeout
	if ok {
		timeout = time.Duration(secs) * time.Second
	}
	timeout = ClampTimeout(timeout)

	runner := BashRunner{Dir: r.guard.Root(), StripEnv: r.opts.StripEnv}
	br := runner.Run(ctx, command, timeout)

	var b strings.Builder
	b.WriteString(br.Output)
	if br.Output != "" && !strings.HasSuffix(br.Output, "\n") {
		b.WriteByte('\n')
	}
	fmt.Fprintf(&b, "[exit code: %d]", br.ExitCode)
	switch {
	case br.TimedOut:
		fmt.Fprintf(&b, " [timed out after %ds]", int(timeout/time.Second))
	case br.Canceled:
		b.WriteString(" [cancelled]")
	}
	return Result{Output: b.String(), IsError: br.ExitCode != 0}
}
