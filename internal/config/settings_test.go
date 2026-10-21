package config

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// settingsPath returns the user config path under a fresh temp HOME.
func settingsPath(t *testing.T) string {
	t.Helper()
	return GlobalPath(t.TempDir())
}

func writeSettingsFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

func readSettingsFile(t *testing.T, path string) []byte {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// assertOnlyFile fails when the directory of path holds anything but path
// (for example a leftover temp file).
func assertOnlyFile(t *testing.T, path string) {
	t.Helper()
	entries, err := os.ReadDir(filepath.Dir(path))
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if e.Name() != filepath.Base(path) {
			t.Errorf("unexpected file left in config dir: %s", e.Name())
		}
	}
}

func TestSaveSettingsThreeKeysAtomic(t *testing.T) {
	path := settingsPath(t)
	writeSettingsFile(t, path, `{
  "default_model": "old/model",
  "effort": "low",
  "max_subagents": 7,
  "auto_compact": false,
  "big": 12345678901234567890,
  "unknown_thing": {"nested": [1, 2.50, "x<y"]},
  "provider": {"p": {"options": {"baseURL": "http://127.0.0.1:1", "apiKey": "{env:SOME_KEY}"}}}
}`)
	if err := SaveSettings(path, "p/new", "high", "auto"); err != nil {
		t.Fatalf("SaveSettings: %v", err)
	}
	data := readSettingsFile(t, path)
	if !bytes.HasSuffix(data, []byte("}\n")) {
		t.Errorf("no trailing newline: %q", data)
	}
	var got map[string]json.RawMessage
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("written file does not parse: %v", err)
	}
	want := map[string]string{
		SettingsKeyModel:  `"p/new"`,
		SettingsKeyEffort: `"high"`,
		SettingsKeyMode:   `"auto"`,
		"max_subagents":   `7`,
		"auto_compact":    `false`,
		"big":             `12345678901234567890`,
		"unknown_thing":   `{"nested":[1,2.50,"x<y"]}`,
		"provider":        `{"p":{"options":{"baseURL":"http://127.0.0.1:1","apiKey":"{env:SOME_KEY}"}}}`,
	}
	if len(got) != len(want) {
		t.Errorf("got %d keys, want %d: %s", len(got), len(want), data)
	}
	for k, w := range want {
		var c bytes.Buffer
		if err := json.Compact(&c, got[k]); err != nil {
			t.Fatalf("key %q: %v", k, err)
		}
		if c.String() != w {
			t.Errorf("key %q = %s, want %s", k, c.String(), w)
		}
	}
	if SettingsKeyModel != "default_model" || SettingsKeyEffort != "effort" || SettingsKeyMode != "mode" {
		t.Errorf("settings keys changed: %q %q %q", SettingsKeyModel, SettingsKeyEffort, SettingsKeyMode)
	}
	assertOnlyFile(t, path)
	fi, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if perm := fi.Mode().Perm(); perm != 0o600 {
		t.Errorf("file mode = %o, want 600", perm)
	}
	// The saved model is what a later Load picks up.
	cfg, err := Load(filepath.Dir(filepath.Dir(filepath.Dir(path))), t.TempDir(), func(string) string { return "k" })
	if err != nil {
		t.Fatalf("Load after save: %v", err)
	}
	if cfg.DefaultModel != "p/new" || cfg.Raw["effort"] != "high" || cfg.Raw["mode"] != "auto" {
		t.Errorf("Load after save: model %q effort %v mode %v", cfg.DefaultModel, cfg.Raw["effort"], cfg.Raw["mode"])
	}
}

func TestSaveSettingsMissingFileCreates(t *testing.T) {
	path := settingsPath(t) // parent directory does not exist yet
	if err := SaveSettings(path, "p/m", "medium", "manual"); err != nil {
		t.Fatalf("SaveSettings: %v", err)
	}
	data := readSettingsFile(t, path)
	var got map[string]any
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("written file does not parse: %v", err)
	}
	want := map[string]any{SettingsKeyModel: "p/m", SettingsKeyEffort: "medium", SettingsKeyMode: "manual"}
	if len(got) != len(want) {
		t.Fatalf("got keys %v, want exactly %v", got, want)
	}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("key %q = %v, want %v", k, got[k], v)
		}
	}
	fi, err := os.Stat(filepath.Dir(path))
	if err != nil {
		t.Fatal(err)
	}
	if perm := fi.Mode().Perm(); perm != 0o700 {
		t.Errorf("dir mode = %o, want 700", perm)
	}
	assertOnlyFile(t, path)
}

func TestSaveSettingsInvalidJSONFileNotOverwritten(t *testing.T) {
	for _, content := range []string{"not json", "null", "[1, 2]", ""} {
		path := settingsPath(t)
		writeSettingsFile(t, path, content)
		err := SaveSettings(path, "p/m", "high", "auto")
		if err == nil || !strings.Contains(err.Error(), "could not save settings") {
			t.Errorf("content %q: err = %v, want could not save settings", content, err)
		}
		if got := readSettingsFile(t, path); string(got) != content {
			t.Errorf("content %q: file changed to %q", content, got)
		}
		assertOnlyFile(t, path)
	}
}

func TestSaveSettingsPreservesEnvTokenLiteral(t *testing.T) {
	t.Setenv("SECRET_VAR", "SECRET123")
	path := settingsPath(t)
	writeSettingsFile(t, path, `{"api_key": "{env:SECRET_VAR}", "custom": true}`)
	if err := SaveSettings(path, "p/m", "low", "manual"); err != nil {
		t.Fatalf("SaveSettings: %v", err)
	}
	data := string(readSettingsFile(t, path))
	if !strings.Contains(data, `"api_key": "{env:SECRET_VAR}"`) {
		t.Errorf("env token not preserved literally: %s", data)
	}
	if !strings.Contains(data, `"custom": true`) {
		t.Errorf("custom key not preserved: %s", data)
	}
	if strings.Contains(data, "SECRET123") {
		t.Errorf("resolved secret written to settings file")
	}
}

func TestSaveSettingsNoResolvedSecretWritten(t *testing.T) {
	r := NewRedactor("SECRET123")
	if got := r.Redact("x SECRET123 y"); got != "x "+Redacted+" y" {
		t.Fatalf("redactor setup: %q", got)
	}
	path := settingsPath(t)
	writeSettingsFile(t, path, `{"provider": {"p": {"options": {"apiKey": "{env:SECRET_VAR}"}}}, "note": "plain"}`)
	if err := SaveSettings(path, "p/m", "high", "auto"); err != nil {
		t.Fatalf("SaveSettings: %v", err)
	}
	data := string(readSettingsFile(t, path))
	if strings.Contains(data, "SECRET123") {
		t.Errorf("resolved secret written to settings file")
	}
	if strings.Contains(data, Redacted) {
		t.Errorf("redaction marker written to settings file: %s", data)
	}
	if !strings.Contains(data, "{env:SECRET_VAR}") {
		t.Errorf("env token lost: %s", data)
	}
}

func TestSaveSettingsIdempotent(t *testing.T) {
	path := settingsPath(t)
	writeSettingsFile(t, path, `{"z": 1, "a": {"b": "{env:X}"}, "effort": "low"}`)
	if err := SaveSettings(path, "p/m", "medium", "auto"); err != nil {
		t.Fatalf("first SaveSettings: %v", err)
	}
	first := readSettingsFile(t, path)
	if err := SaveSettings(path, "p/m", "medium", "auto"); err != nil {
		t.Fatalf("second SaveSettings: %v", err)
	}
	second := readSettingsFile(t, path)
	if !bytes.Equal(first, second) {
		t.Errorf("content changed between identical saves:\n%s\n---\n%s", first, second)
	}
	assertOnlyFile(t, path)
}
