package config

import (
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// leftoverKey is the top-level key of the removed turn-routing block. It is
// built from an escape so the name never appears in the source.
const leftoverKey = "\x6aev"

const leftoverProvider = `"provider":{"fake":{"options":{"baseURL":"http://127.0.0.1:9/v1","apiKey":"k"},"models":{"m1":{}}}}`

// leftoverBlock is ill-typed under the old rules for that key.
const leftoverBlock = `{"baseURL":"https://leftover.example","apiKey":"literal","model":7,"min_confidence":"bad","route":{"easy":"nope/x"}}`

// loadLeftover loads a global config with the fake provider, plus block under
// the leftover key in the global file (inProject false) or in the project file.
func loadLeftover(t *testing.T, block string, inProject bool, env map[string]string) (*Config, error) {
	t.Helper()
	home, dir := t.TempDir(), t.TempDir()
	entry := `"` + leftoverKey + `":` + block
	if inProject {
		writeFile(t, GlobalPath(home), `{`+leftoverProvider+`}`)
		writeFile(t, filepath.Join(dir, ProjectFile), `{`+entry+`}`)
	} else {
		writeFile(t, GlobalPath(home), `{`+leftoverProvider+`,`+entry+`}`)
	}
	return Load(home, dir, fakeEnv(env))
}

func TestConfigLeftoverKey(t *testing.T) {
	for _, inProject := range []bool{false, true} {
		cfg, err := loadLeftover(t, leftoverBlock, inProject, map[string]string{})
		if err != nil {
			t.Fatalf("inProject=%v: Load: %v", inProject, err)
		}
		if got, want := cfg.BaseURLs(), []string{"http://127.0.0.1:9/v1"}; !slices.Equal(got, want) {
			t.Fatalf("inProject=%v: BaseURLs = %q, want %q", inProject, got, want)
		}
		if got := cfg.DefaultModelID(); got != "fake/m1" {
			t.Fatalf("inProject=%v: DefaultModelID = %q, want fake/m1", inProject, got)
		}
	}

	cfg, err := loadLeftover(t, `{"apiKey":"{env:LEFTOVER_SET}"}`, false, map[string]string{"LEFTOVER_SET": "SECRET123"})
	if err != nil {
		t.Fatalf("set env: Load: %v", err)
	}
	if got := cfg.Redactor.Redact("k SECRET123"); got != "k [redacted]" {
		t.Fatalf("Redact = %q, want %q", got, "k [redacted]")
	}

	_, err = loadLeftover(t, `{"apiKey":"{env:LEFTOVER_UNSET}"}`, false, map[string]string{})
	if err == nil || !strings.Contains(err.Error(), "LEFTOVER_UNSET") {
		t.Fatalf("unset env: err = %v, want one naming LEFTOVER_UNSET", err)
	}
}
