package config

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestLoadGlobal(t *testing.T) {
	home, dir := t.TempDir(), t.TempDir()
	writeFile(t, GlobalPath(home), `{"default_model":"p/m","max_subagents":7,"auto_compact":false,"extra":{"a":1}}`)
	cfg, err := Load(home, dir, nil)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.DefaultModel != "p/m" || cfg.MaxSubagents != 7 || cfg.AutoCompact {
		t.Fatalf("unexpected config: %+v", cfg)
	}
	if _, ok := cfg.Raw["extra"]; !ok {
		t.Fatalf("unknown key not kept in Raw: %v", cfg.Raw)
	}
}

func TestProjectOverride(t *testing.T) {
	home, dir := t.TempDir(), t.TempDir()
	writeFile(t, GlobalPath(home), `{
		"default_model":"global/m",
		"provider":{"p":{"base_url":"http://x","models":{"a":{"n":1},"b":{"n":2}}}}
	}`)
	writeFile(t, filepath.Join(dir, ProjectFile), `{
		"default_model":"proj/m",
		"provider":{"p":{"models":{"b":{"n":3},"c":{"n":4}}}}
	}`)
	cfg, err := Load(home, dir, nil)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.DefaultModel != "proj/m" {
		t.Fatalf("scalar not replaced: %q", cfg.DefaultModel)
	}
	p := cfg.Raw["provider"].(map[string]any)["p"].(map[string]any)
	if p["base_url"] != "http://x" {
		t.Fatalf("global key lost: %v", p)
	}
	models := p["models"].(map[string]any)
	want := map[string]any{
		"a": map[string]any{"n": 1.0},
		"b": map[string]any{"n": 3.0},
		"c": map[string]any{"n": 4.0},
	}
	if !reflect.DeepEqual(models, want) {
		t.Fatalf("models = %v, want %v", models, want)
	}
}

func TestLoadMissingFiles(t *testing.T) {
	cfg, err := Load(t.TempDir(), t.TempDir(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.MaxSubagents != 4 || !cfg.AutoCompact || cfg.DefaultModel != "" {
		t.Fatalf("unexpected defaults: %+v", cfg)
	}
	if cfg.Raw == nil || len(cfg.Raw) != 0 {
		t.Fatalf("Raw = %v, want empty object", cfg.Raw)
	}
}

func TestLoadInvalidJSON(t *testing.T) {
	home, dir := t.TempDir(), t.TempDir()
	writeFile(t, filepath.Join(dir, ProjectFile), `{"secret_marker": oops}`)
	_, err := Load(home, dir, nil)
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), ProjectFile) {
		t.Fatalf("error does not name the file: %v", err)
	}
	if strings.Contains(err.Error(), "secret_marker") {
		t.Fatalf("error leaks file contents: %v", err)
	}

	writeFile(t, filepath.Join(dir, ProjectFile), `[1,2]`)
	if _, err := Load(home, dir, nil); err == nil || !strings.Contains(err.Error(), ProjectFile) {
		t.Fatalf("non-object top level: %v", err)
	}

	writeFile(t, filepath.Join(dir, ProjectFile), `{"max_subagents":"x"}`)
	if _, err := Load(home, dir, nil); err == nil || !strings.Contains(err.Error(), "max_subagents") {
		t.Fatalf("wrong type: %v", err)
	}
}

func TestMergeDoesNotMutate(t *testing.T) {
	base := map[string]any{"a": map[string]any{"x": 1.0, "y": 2.0}, "l": []any{1.0}}
	over := map[string]any{"a": map[string]any{"y": 3.0, "z": 4.0}, "l": []any{2.0}}
	baseCopy := map[string]any{"a": map[string]any{"x": 1.0, "y": 2.0}, "l": []any{1.0}}
	overCopy := map[string]any{"a": map[string]any{"y": 3.0, "z": 4.0}, "l": []any{2.0}}

	got := Merge(base, over)
	want := map[string]any{"a": map[string]any{"x": 1.0, "y": 3.0, "z": 4.0}, "l": []any{2.0}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Merge = %v, want %v", got, want)
	}
	got["a"].(map[string]any)["x"] = 99.0
	if !reflect.DeepEqual(base, baseCopy) || !reflect.DeepEqual(over, overCopy) {
		t.Fatalf("inputs mutated: base=%v over=%v", base, over)
	}
}
