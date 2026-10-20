package config

import (
	"errors"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// fakeEnv returns an injected getenv; the real environment is never read.
func fakeEnv(m map[string]string) func(string) string {
	return func(k string) string { return m[k] }
}

func TestEnvSubstitution(t *testing.T) {
	home, dir := t.TempDir(), t.TempDir()
	writeFile(t, GlobalPath(home), `{
		"default_model": "{env:A}/m",
		"list": ["x{env:B}y", {"deep": "{env:A}-{env:B}-{env:A}"}, 3, true, null],
		"literal": "{env:not-a-name} {env:} {ENV:A} {env:1X}",
		"{env:A}": "keys are not substituted",
		"nested": {"again": "{env:C}"}
	}`)
	env := map[string]string{"A": "aa", "B": "{env:C}", "C": "cc"}
	cfg, err := Load(home, dir, fakeEnv(env))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.DefaultModel != "aa/m" {
		t.Fatalf("default_model = %q", cfg.DefaultModel)
	}
	list := cfg.Raw["list"].([]any)
	if list[0] != "x{env:C}y" {
		t.Fatalf("substituted text must not be re-scanned: %q", list[0])
	}
	if d := list[1].(map[string]any)["deep"]; d != "aa-{env:C}-aa" {
		t.Fatalf("deep = %q", d)
	}
	if list[2] != float64(3) || list[3] != true || list[4] != nil {
		t.Fatalf("non-string values changed: %v", list)
	}
	if l := cfg.Raw["literal"]; l != "{env:not-a-name} {env:} {ENV:A} {env:1X}" {
		t.Fatalf("invalid references must stay literal: %q", l)
	}
	if _, ok := cfg.Raw["{env:A}"]; !ok {
		t.Fatalf("object keys must not be substituted: %v", cfg.Raw)
	}
	if v := cfg.Raw["nested"].(map[string]any)["again"]; v != "cc" {
		t.Fatalf("nested = %q", v)
	}
	if !reflect.DeepEqual(cfg.EnvNames, []string{"A", "B", "C"}) {
		t.Fatalf("EnvNames = %v", cfg.EnvNames)
	}
	for _, v := range []string{"aa", "cc", "{env:C}"} {
		if got := cfg.Redactor.Redact("<" + v + ">"); got != "<"+Redacted+">" {
			t.Fatalf("resolved value not registered: %q", got)
		}
	}

	// Without references: empty EnvNames, a non-nil Redactor, nil getenv allowed.
	home2, dir2 := t.TempDir(), t.TempDir()
	cfg2, err := Load(home2, dir2, nil)
	if err != nil || cfg2.Redactor == nil || len(cfg2.EnvNames) != 0 {
		t.Fatalf("plain load: err=%v cfg=%+v", err, cfg2)
	}
}

func TestMissingEnvNamesVariable(t *testing.T) {
	home, dir := t.TempDir(), t.TempDir()
	writeFile(t, GlobalPath(home), `{"provider": {
		"a": {"name": "{env:SET}", "options": {"apiKey": "{env:SET}"}},
		"b": {"options": {"apiKey": "{env:ZZ_MISSING}", "baseURL": "{env:AA_MISSING}"}}
	}}`)
	_, err := Load(home, dir, fakeEnv(map[string]string{"SET": "SECRET123"}))
	var me *MissingEnvError
	if !errors.As(err, &me) {
		t.Fatalf("want *MissingEnvError, got %v", err)
	}
	// Deterministic: sorted key order puts options.apiKey before options.baseURL.
	if me.Name != "ZZ_MISSING" {
		t.Fatalf("Name = %q", me.Name)
	}
	msg := err.Error()
	if msg != "environment variable ZZ_MISSING is not set or empty" {
		t.Fatalf("message = %q", msg)
	}
	if strings.Contains(msg, "SECRET123") || strings.Contains(msg, "provider") {
		t.Fatalf("message leaks values or config content: %q", msg)
	}
	for i := 0; i < 20; i++ {
		_, err2 := Load(home, dir, fakeEnv(map[string]string{"SET": "SECRET123"}))
		if err2 == nil || err2.Error() != msg {
			t.Fatalf("non-deterministic error: %v vs %q", err2, msg)
		}
	}
	// Also in the project file.
	home2, dir2 := t.TempDir(), t.TempDir()
	writeFile(t, filepath.Join(dir2, ProjectFile), `{"x": ["ok", "{env:IN_PROJECT}"]}`)
	if _, err := Load(home2, dir2, fakeEnv(nil)); !errors.As(err, &me) || me.Name != "IN_PROJECT" {
		t.Fatalf("project reference: %v", err)
	}
}

func TestEmptyEnvIsMissing(t *testing.T) {
	home, dir := t.TempDir(), t.TempDir()
	writeFile(t, GlobalPath(home), `{"provider": {"p": {"options": {"apiKey": "prefix-{env:EMPTY}"}}}}`)
	_, err := Load(home, dir, fakeEnv(map[string]string{"EMPTY": ""}))
	var me *MissingEnvError
	if !errors.As(err, &me) || me.Name != "EMPTY" {
		t.Fatalf("want *MissingEnvError{EMPTY}, got %v", err)
	}
	if !strings.Contains(err.Error(), "EMPTY") || strings.Contains(err.Error(), "prefix") {
		t.Fatalf("message = %q", err.Error())
	}
}

func TestCostIgnored(t *testing.T) {
	home, dir := t.TempDir(), t.TempDir()
	writeFile(t, GlobalPath(home), `{"provider": {"p": {
		"npm": "@ai-sdk/openai-compatible",
		"unknown": [1, 2],
		"options": {"baseURL": "http://127.0.0.1:9/v1", "headers": {"x": 1}, "timeout": 5},
		"models": {"m": {"cost": {"input": 1.5, "output": "free"}, "extra": 7}}
	}}}`)
	cfg, err := Load(home, dir, fakeEnv(nil))
	if err != nil {
		t.Fatal(err)
	}
	_, m, ok := cfg.Lookup("p/m")
	if !ok || m != (Model{ID: "p/m", Provider: "p", Key: "m", Reasoning: true}) {
		t.Fatalf("model = %+v ok=%v", m, ok)
	}
}

func TestModelParsing(t *testing.T) {
	home, dir := t.TempDir(), t.TempDir()
	writeFile(t, GlobalPath(home), `{"provider": {
		"fake": {"name": "Fake", "options": {"baseURL": "http://127.0.0.1:9/v1//", "apiKey": "k"},
			"models": {"m1": {"name": "M1", "limit": {"context": 8000, "output": 10}},
			           "org/m2": {"reasoning": false}}},
		"other": {"options": {"baseURL": "http://127.0.0.1:9/v1/"}, "models": {"x": {}}},
		"bare": {}
	}}`)
	writeFile(t, filepath.Join(dir, ProjectFile), `{"provider": {"fake": {"models": {"m3": {"name": "M3"}}}}}`)
	cfg, err := Load(home, dir, fakeEnv(nil))
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"fake/m1", "fake/m3", "fake/org/m2", "other/x"}
	if ids := cfg.ModelIDs(); !reflect.DeepEqual(ids, want) {
		t.Fatalf("ModelIDs = %v", ids)
	}
	p, m1, ok := cfg.Lookup("fake/m1")
	if !ok || p.ID != "fake" || p.Name != "Fake" || p.BaseURL != "http://127.0.0.1:9/v1" || p.APIKey != "k" {
		t.Fatalf("provider = %+v", p)
	}
	if m1 != (Model{ID: "fake/m1", Provider: "fake", Key: "m1", Name: "M1", Reasoning: true, ContextLimit: 8000}) {
		t.Fatalf("m1 = %+v", m1)
	}
	if _, m2, ok := cfg.Lookup("fake/org/m2"); !ok || m2.Key != "org/m2" || m2.Reasoning {
		t.Fatalf("m2 = %+v ok=%v", m2, ok)
	}
	for _, id := range []string{"", "fake", "fake/", "/m1", "nope/m1", "fake/m9"} {
		if _, _, ok := cfg.Lookup(id); ok {
			t.Fatalf("Lookup(%q) succeeded", id)
		}
	}
	if b := cfg.BaseURLs(); !reflect.DeepEqual(b, []string{"http://127.0.0.1:9/v1"}) {
		t.Fatalf("BaseURLs = %v", b)
	}
	if bare := cfg.Providers["bare"]; bare.ID != "bare" || bare.Models == nil || len(bare.Models) != 0 {
		t.Fatalf("bare = %+v", bare)
	}

	// Wrong types: the error names the JSON path and never the value.
	bad := map[string]string{
		`{"provider": "SECRET123"}`:                                             `"provider"`,
		`{"provider": {"p": ["SECRET123"]}}`:                                    `"provider.p"`,
		`{"provider": {"p": {"name": 5}}}`:                                      `"provider.p.name"`,
		`{"provider": {"p": {"options": "SECRET123"}}}`:                         `"provider.p.options"`,
		`{"provider": {"p": {"options": {"baseURL": ["SECRET123"]}}}}`:          `"provider.p.options.baseURL"`,
		`{"provider": {"p": {"options": {"apiKey": {"v": "SECRET123"}}}}}`:      `"provider.p.options.apiKey"`,
		`{"provider": {"p": {"models": []}}}`:                                   `"provider.p.models"`,
		`{"provider": {"p": {"models": {"m": "SECRET123"}}}}`:                   `"provider.p.models.m"`,
		`{"provider": {"p": {"models": {"m": {"name": true}}}}}`:                `"provider.p.models.m.name"`,
		`{"provider": {"p": {"models": {"m": {"reasoning": "SECRET123"}}}}}`:    `"provider.p.models.m.reasoning"`,
		`{"provider": {"p": {"models": {"m": {"limit": 1}}}}}`:                  `"provider.p.models.m.limit"`,
		`{"provider": {"p": {"models": {"m": {"limit": {"context": "8k"}}}}}}`:  `"provider.p.models.m.limit.context"`,
		`{"provider": {"p": {"models": {"m": {"limit": {"context": 1.5}}}}}}`:   `"provider.p.models.m.limit.context"`,
		`{"provider": {"p": {"models": {"m": {"limit": {"context": -1}}}}}}`:    `"provider.p.models.m.limit.context"`,
		`{"provider": {"a/b": {"name": "SECRET123"}}}`:                          `"provider"`,
		`{"provider": {"p": {"options": {"apiKey": "{env:K}"}, "name": [1]}}}`:  `"provider.p.name"`,
		`{"provider": {"p": {"options": {"apiKey": "{env:K}", "baseURL": 1}}}}`: `"provider.p.options.baseURL"`,
		`{"default_model": "{env:K}", "provider": {"p": {"models": {"": {}}}}}`: `"provider.p.models"`,
	}
	for content, path := range bad {
		home, dir := t.TempDir(), t.TempDir()
		writeFile(t, GlobalPath(home), content)
		_, err := Load(home, dir, fakeEnv(map[string]string{"K": "SECRET123"}))
		if err == nil {
			t.Fatalf("accepted %s", content)
		}
		if !strings.Contains(err.Error(), path) || strings.Contains(err.Error(), "SECRET123") {
			t.Fatalf("%s: error %q must name %s and contain no value", content, err, path)
		}
	}
}

func TestDefaultModelID(t *testing.T) {
	cases := []struct {
		content, want string
	}{
		{`{}`, ""},
		{`{"default_model": "p/b"}`, ""},
		{`{"provider": {"p": {"models": {"b": {}, "a": {}}}}}`, "p/a"},
		{`{"default_model": "p/b", "provider": {"p": {"models": {"b": {}, "a": {}}}}}`, "p/b"},
		{`{"default_model": "p/zz", "provider": {"p": {"models": {"b": {}, "a": {}}}}}`, "p/a"},
		{`{"default_model": "", "provider": {"q": {"models": {"x": {}}}, "p": {}}}`, "q/x"},
	}
	for _, tc := range cases {
		home, dir := t.TempDir(), t.TempDir()
		writeFile(t, GlobalPath(home), tc.content)
		cfg, err := Load(home, dir, fakeEnv(nil))
		if err != nil {
			t.Fatalf("%s: %v", tc.content, err)
		}
		if got := cfg.DefaultModelID(); got != tc.want {
			t.Fatalf("%s: DefaultModelID = %q, want %q", tc.content, got, tc.want)
		}
	}
}

func TestSecretConfig(t *testing.T) {
	env := fakeEnv(map[string]string{"YIP_KEY": "SECRET123", "OTHER": "other-value"})
	home, dir := t.TempDir(), t.TempDir()
	writeFile(t, GlobalPath(home), `{"provider": {"p": {
		"options": {"baseURL": "http://127.0.0.1:9/{env:OTHER}", "apiKey": "{env:YIP_KEY}"},
		"models": {"m": {}}}}}`)
	cfg, err := Load(home, dir, env)
	if err != nil {
		t.Fatal(err)
	}
	p, _, _ := cfg.Lookup("p/m")
	if p.APIKey != "SECRET123" {
		t.Fatal("apiKey not resolved")
	}
	if got := cfg.Redactor.Redact("Bearer SECRET123 and other-value"); got != "Bearer "+Redacted+" and "+Redacted {
		t.Fatalf("Redact = %q", got)
	}
	for _, s := range []string{cfg.Redactor.String(), cfg.Redactor.GoString()} {
		if strings.Contains(s, "SECRET123") {
			t.Fatal("Redactor prints its secret")
		}
	}

	// Every error Load returns must be free of the resolved secret.
	failing := map[string]string{
		"missing":   `{"a": "{env:YIP_KEY}", "b": "{env:UNSET}"}`,
		"type":      `{"provider": {"p": {"name": "{env:YIP_KEY}", "options": {"apiKey": "{env:YIP_KEY}", "baseURL": 1}}}}`,
		"default":   `{"default_model": ["{env:YIP_KEY}"]}`,
		"subagents": `{"max_subagents": "{env:YIP_KEY}"}`,
		"compact":   `{"auto_compact": "{env:YIP_KEY}"}`,
		"models":    `{"provider": {"p": {"models": "{env:YIP_KEY}"}}}`,
	}
	for name, content := range failing {
		home, dir := t.TempDir(), t.TempDir()
		writeFile(t, GlobalPath(home), content)
		_, err := Load(home, dir, env)
		if err == nil {
			t.Fatalf("%s: no error", name)
		}
		if strings.Contains(err.Error(), "SECRET123") {
			t.Fatalf("%s: error leaks the secret: %q", name, err)
		}
	}
	// Malformed project JSON: names the file, never the content.
	home2, dir2 := t.TempDir(), t.TempDir()
	writeFile(t, GlobalPath(home2), `{"provider": {"p": {"options": {"apiKey": "{env:YIP_KEY}"}}}}`)
	writeFile(t, filepath.Join(dir2, ProjectFile), `{"default_model": SECRET123`)
	_, err = Load(home2, dir2, env)
	if err == nil || !strings.Contains(err.Error(), ProjectFile) || strings.Contains(err.Error(), "SECRET123") {
		t.Fatalf("malformed project: %v", err)
	}
}
