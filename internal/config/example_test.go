package config

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

// TestExampleConfigLoads checks that config.example.json loads through Load
// with the documented values. The expected base URL is read from the example
// file itself, so no provider host is hard-coded here.
func TestExampleConfigLoads(t *testing.T) {
	example, err := os.ReadFile(filepath.Join("..", "..", "config.example.json"))
	if err != nil {
		t.Fatal(err)
	}
	var want struct {
		Provider map[string]struct {
			Options struct {
				BaseURL string `json:"baseURL"`
			} `json:"options"`
		} `json:"provider"`
	}
	if err := json.Unmarshal(example, &want); err != nil {
		t.Fatal(err)
	}
	wantBase := want.Provider["openrouter"].Options.BaseURL
	if wantBase == "" {
		t.Fatal("example file has no openrouter baseURL")
	}

	load := func(getenv func(string) string) (*Config, error) {
		home := t.TempDir()
		writeFile(t, GlobalPath(home), string(example))
		return Load(home, t.TempDir(), getenv)
	}

	cfg, err := load(func(name string) string {
		if name == "OPENROUTER_API_KEY" {
			return "dummy-value"
		}
		return ""
	})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.MaxSubagents != 4 {
		t.Errorf("MaxSubagents = %d, want 4", cfg.MaxSubagents)
	}
	if !cfg.AutoCompact {
		t.Error("AutoCompact = false, want true")
	}
	p, ok := cfg.Providers["openrouter"]
	if !ok {
		t.Fatalf("provider openrouter missing: %v", cfg.Providers)
	}
	if p.BaseURL != wantBase {
		t.Errorf("BaseURL = %q, want %q", p.BaseURL, wantBase)
	}
	if p.APIKey != "dummy-value" {
		t.Errorf("APIKey not resolved from environment")
	}
	if len(p.Models) != 0 {
		t.Errorf("Models = %v, want none", p.Models)
	}
	if id := cfg.DefaultModelID(); id != "" {
		t.Errorf("DefaultModelID() = %q, want empty", id)
	}

	_, err = load(func(string) string { return "" })
	var missing *MissingEnvError
	if !errors.As(err, &missing) {
		t.Fatalf("unset variable: error = %v, want *MissingEnvError", err)
	}
	if missing.Name != "OPENROUTER_API_KEY" {
		t.Errorf("MissingEnvError.Name = %q, want OPENROUTER_API_KEY", missing.Name)
	}
}
