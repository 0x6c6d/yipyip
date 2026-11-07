package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadGlobalDir(t *testing.T) {
	home := t.TempDir()
	start := t.TempDir()
	dir := filepath.Join(home, ".config", "yipyip")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "config.json"), []byte(`{"max_subagents": 2}`), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(home, start, func(string) string { return "" })
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.GlobalDir != dir {
		t.Fatalf("GlobalDir = %q, want %q", cfg.GlobalDir, dir)
	}
	if got := GlobalAgentsPath(home); got != filepath.Join(dir, "AGENTS.md") {
		t.Fatalf("GlobalAgentsPath = %q", got)
	}
}

func TestLoadGlobalDirMissingConfig(t *testing.T) {
	home := t.TempDir()
	start := t.TempDir()
	cfg, err := Load(home, start, func(string) string { return "" })
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	want := filepath.Join(home, ".config", "yipyip")
	if cfg.GlobalDir != want {
		t.Fatalf("GlobalDir = %q, want %q", cfg.GlobalDir, want)
	}
	if _, err := os.Stat(want); !os.IsNotExist(err) {
		t.Fatalf("Load created %s (stat err %v)", want, err)
	}
}

func TestLoadGlobalDirIgnoresXDG(t *testing.T) {
	home := t.TempDir()
	start := t.TempDir()
	cfg, err := Load(home, start, func(k string) string {
		if k == "XDG_CONFIG_HOME" {
			return "/elsewhere"
		}
		return ""
	})
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	want := filepath.Join(home, ".config", "yipyip")
	if cfg.GlobalDir != want {
		t.Fatalf("GlobalDir = %q, want %q", cfg.GlobalDir, want)
	}
}
