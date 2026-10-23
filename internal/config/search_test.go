package config

import (
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

const searchProviders = `"provider":{"fake":{"options":{"baseURL":"http://127.0.0.1:9/v1","apiKey":"k"},"models":{"m1":{}}}}`

// loadSearch loads a global config with the fake provider plus extra
// (appended to the global object when non-empty) and an optional project file.
func loadSearch(t *testing.T, extra, project string, env map[string]string) (*Config, error) {
	t.Helper()
	home, dir := t.TempDir(), t.TempDir()
	global := `{` + searchProviders
	if extra != "" {
		global += `,` + extra
	}
	writeFile(t, GlobalPath(home), global+`}`)
	if project != "" {
		writeFile(t, filepath.Join(dir, ProjectFile), project)
	}
	return Load(home, dir, fakeEnv(env))
}

var braveEnv = map[string]string{"BRAVE_API_KEY": "BRAVEVAL1"}

func TestSearchConfigLoad(t *testing.T) {
	cfg, err := loadSearch(t, `"search":{"apiKey":"{env:BRAVE_API_KEY}"}`, "", braveEnv)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Search == nil || cfg.Search.APIKey != "BRAVEVAL1" {
		t.Fatalf("Search = %+v", cfg.Search)
	}
	if !slices.Contains(cfg.EnvNames, "BRAVE_API_KEY") {
		t.Fatalf("EnvNames = %v", cfg.EnvNames)
	}
	if got := cfg.Redactor.Redact("key BRAVEVAL1 end"); strings.Contains(got, "BRAVEVAL1") {
		t.Fatalf("search apiKey not redacted: %q", got)
	}
}

func TestSearchConfigDefaults(t *testing.T) {
	cfg, err := loadSearch(t, `"search":{"apiKey":"{env:BRAVE_API_KEY}"}`, "", braveEnv)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Search == nil || cfg.Search.BaseURL != DefaultSearchBaseURL {
		t.Fatalf("default baseURL: %+v", cfg.Search)
	}
	for _, c := range []struct{ in, want string }{
		{"http://127.0.0.1:8080/", "http://127.0.0.1:8080"},
		{"http://localhost:9", "http://localhost:9"},
		{"http://[::1]:7/", "http://[::1]:7"},
		{"https://search.example.org/", "https://search.example.org"},
	} {
		cfg, err := loadSearch(t, `"search":{"apiKey":"{env:BRAVE_API_KEY}","baseURL":"`+c.in+`"}`, "", braveEnv)
		if err != nil {
			t.Fatalf("%s: %v", c.in, err)
		}
		if cfg.Search == nil || cfg.Search.BaseURL != c.want {
			t.Fatalf("%s: %+v, want %s", c.in, cfg.Search, c.want)
		}
	}
}

func TestSearchConfigAbsent(t *testing.T) {
	cfg, err := loadSearch(t, "", "", braveEnv)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Search != nil {
		t.Fatalf("Search = %+v, want nil", cfg.Search)
	}
}

func TestSearchConfigMissingKey(t *testing.T) {
	for _, s := range []string{`{}`, `{"apiKey":""}`, `{"apiKey":7}`} {
		_, err := loadSearch(t, `"search":`+s, "", braveEnv)
		if err == nil || !strings.Contains(err.Error(), "search.apiKey") {
			t.Fatalf("%s: err = %v, want one naming search.apiKey", s, err)
		}
	}
	_, err := loadSearch(t, `"search":{"apiKey":"{env:BRAVE_API_KEY}"}`, "", map[string]string{})
	if err == nil || !strings.Contains(err.Error(), "BRAVE_API_KEY") {
		t.Fatalf("unset env: err = %v, want one naming BRAVE_API_KEY", err)
	}
}

func TestSearchConfigNonHTTPS(t *testing.T) {
	for _, c := range []struct{ search, key, host string }{
		{`{"apiKey":"k","baseURL":"http://example.com"}`, "search.baseURL", "example.com"},
		{`{"apiKey":"k","baseURL":"http://127.0.0.2:9"}`, "search.baseURL", "127.0.0.2"},
		{`{"apiKey":"k","baseURL":"http://localhost.example.com"}`, "search.baseURL", "localhost.example.com"},
		{`{"apiKey":"k","baseURL":"ftp://127.0.0.1"}`, "search.baseURL", "ftp:"},
		{`{"apiKey":"k","baseURL":"https://"}`, "search.baseURL", "https://"},
		{`{"apiKey":"k","baseURL":7}`, "search.baseURL", ""},
		{`"on"`, "search", ""},
	} {
		_, err := loadSearch(t, `"search":`+c.search, "", braveEnv)
		if err == nil {
			t.Fatalf("%s: accepted", c.search)
		}
		msg := err.Error()
		if !strings.Contains(msg, `"`+c.key+`"`) {
			t.Fatalf("%s: error %q does not name %s", c.search, msg, c.key)
		}
		if c.host != "" && strings.Contains(msg, c.host) {
			t.Fatalf("%s: error %q echoes %q", c.search, msg, c.host)
		}
	}
}

func TestSearchConfigNoEcho(t *testing.T) {
	env := map[string]string{"BRAVE_API_KEY": "SECRET123"}
	url := "http://evil.example.net/SECRET123"
	_, err := loadSearch(t, `"search":{"apiKey":"{env:BRAVE_API_KEY}","baseURL":"`+url+`"}`, "", env)
	if err == nil {
		t.Fatal("invalid baseURL accepted")
	}
	msg := err.Error()
	if strings.Contains(msg, "SECRET123") || strings.Contains(msg, url) || strings.Contains(msg, "evil.example.net") {
		t.Fatalf("error echoes a value: %q", msg)
	}

	cfg, err := loadSearch(t, `"search":{"apiKey":"LITERALKEY1"}`, "", env)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Search == nil || cfg.Search.APIKey != "LITERALKEY1" {
		t.Fatalf("Search = %+v", cfg.Search)
	}
	if got := cfg.Redactor.Redact("k=LITERALKEY1"); strings.Contains(got, "LITERALKEY1") {
		t.Fatalf("literal key not redacted: %q", got)
	}
}

func TestSearchNotInBaseURLs(t *testing.T) {
	cfg, err := loadSearch(t,
		`"search":{"apiKey":"{env:BRAVE_API_KEY}","baseURL":"https://search.example.org/"}`,
		"", braveEnv)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Search == nil {
		t.Fatal("search block not loaded")
	}
	for _, u := range cfg.BaseURLs() {
		if strings.Contains(u, "search.example.org") {
			t.Fatalf("search URL in BaseURLs: %v", cfg.BaseURLs())
		}
	}

	cfg, err = loadSearch(t, `"search":{"apiKey":"k"}`, "", braveEnv)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Search == nil || cfg.Search.BaseURL != DefaultSearchBaseURL {
		t.Fatalf("Search = %+v", cfg.Search)
	}
	for _, u := range cfg.BaseURLs() {
		if u == DefaultSearchBaseURL {
			t.Fatalf("default search URL in BaseURLs: %v", cfg.BaseURLs())
		}
	}
}

func TestSearchConfigProject(t *testing.T) {
	cfg, err := loadSearch(t, "",
		`{"search":{"apiKey":"{env:BRAVE_API_KEY}","baseURL":"http://127.0.0.1:8081/"}}`, braveEnv)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Search == nil || cfg.Search.APIKey != "BRAVEVAL1" || cfg.Search.BaseURL != "http://127.0.0.1:8081" {
		t.Fatalf("Search = %+v", cfg.Search)
	}
}
