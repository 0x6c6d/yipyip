package config

import (
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

const jevProviders = `"provider":{"fake":{"options":{"baseURL":"http://127.0.0.1:9/v1","apiKey":"k"},"models":{"m1":{},"m2":{}}}}`

// loadJev loads a global config with the fake providers and the given "jev"
// value (omitted when jev is ""), plus an optional project file.
func loadJev(t *testing.T, jev, project string) (*Config, error) {
	t.Helper()
	home, dir := t.TempDir(), t.TempDir()
	global := `{` + jevProviders
	if jev != "" {
		global += `,"jev":` + jev
	}
	writeFile(t, GlobalPath(home), global+`}`)
	if project != "" {
		writeFile(t, filepath.Join(dir, ProjectFile), project)
	}
	return Load(home, dir, fakeEnv(map[string]string{"TYPESAFE_API_KEY": "SECRET123"}))
}

func TestJevConfigDefaults(t *testing.T) {
	cfg, err := loadJev(t, `{}`, "")
	if err != nil {
		t.Fatal(err)
	}
	want := Jev{BaseURL: DefaultJevBaseURL, Model: DefaultJevModel, MinConfidence: DefaultJevMinConfidence}
	if cfg.Jev == nil || *cfg.Jev != want {
		t.Fatalf("empty block: %+v, want %+v", cfg.Jev, want)
	}

	cfg, err = loadJev(t, `{"apiKey":"{env:TYPESAFE_API_KEY}"}`, "")
	if err != nil {
		t.Fatal(err)
	}
	want.APIKey = "SECRET123"
	if cfg.Jev == nil || *cfg.Jev != want {
		t.Fatalf("apiKey only: %+v", cfg.Jev)
	}
	if got := cfg.Redactor.Redact("key SECRET123 end"); strings.Contains(got, "SECRET123") {
		t.Fatalf("jev apiKey not redacted: %q", got)
	}
}

func TestJevConfigUnset(t *testing.T) {
	cfg, err := loadJev(t, "", "")
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Jev != nil {
		t.Fatalf("Jev = %+v, want nil", cfg.Jev)
	}
	if len(cfg.ModelIDs()) != 2 {
		t.Fatalf("providers changed: %v", cfg.ModelIDs())
	}
}

func TestJevConfigValues(t *testing.T) {
	cfg, err := loadJev(t, `{"baseURL":"http://127.0.0.1:7/","apiKey":"{env:TYPESAFE_API_KEY}","model":"jx","min_confidence":0.9,"route":{"easy":"fake/m2","hard":"fake/m1"},"extra":1}`, "")
	if err != nil {
		t.Fatal(err)
	}
	j := cfg.Jev
	if j == nil || j.BaseURL != "http://127.0.0.1:7" || j.APIKey != "SECRET123" || j.Model != "jx" || j.MinConfidence != 0.9 {
		t.Fatalf("values: %+v", j)
	}
	if j.Route == nil || *j.Route != (JevRoute{Easy: "fake/m2", Hard: "fake/m1"}) {
		t.Fatalf("route: %+v", j.Route)
	}

	// The project file overrides the global block key by key.
	cfg, err = loadJev(t, `{"baseURL":"http://127.0.0.1:7","model":"jx","min_confidence":0.9}`,
		`{"jev":{"model":"jy","route":{"easy":"fake/m1","hard":"fake/m2"}}}`)
	if err != nil {
		t.Fatal(err)
	}
	j = cfg.Jev
	if j == nil || j.BaseURL != "http://127.0.0.1:7" || j.Model != "jy" || j.MinConfidence != 0.9 || j.APIKey != "" {
		t.Fatalf("merged: %+v", j)
	}
	if j.Route == nil || *j.Route != (JevRoute{Easy: "fake/m1", Hard: "fake/m2"}) {
		t.Fatalf("merged route: %+v", j.Route)
	}
}

// jevReject asserts that loading jev fails with an error naming key and not
// containing any of the forbidden strings.
func jevReject(t *testing.T, jev, key string, forbidden ...string) {
	t.Helper()
	_, err := loadJev(t, jev, "")
	if err == nil {
		t.Fatalf("accepted jev %s", jev)
	}
	msg := err.Error()
	if !strings.HasPrefix(msg, "config: ") || !strings.Contains(msg, `"`+key+`"`) {
		t.Fatalf("error %q does not name %q", msg, key)
	}
	for _, f := range append(forbidden, "SECRET123") {
		if strings.Contains(msg, f) {
			t.Fatalf("error %q contains value %q", msg, f)
		}
	}
}

func TestJevRouteUnknownModel(t *testing.T) {
	jevReject(t, `{"route":{"easy":"fake/ghost","hard":"fake/m1"}}`, "jev.route.easy", "ghost")
	jevReject(t, `{"route":{"easy":"fake/m1","hard":"other/phantom"}}`, "jev.route.hard", "phantom", "other")
	jevReject(t, `{"route":{"easy":"fake/m1"}}`, "jev.route.hard")
	jevReject(t, `{"route":{"easy":"{env:TYPESAFE_API_KEY}","hard":"fake/m1"}}`, "jev.route.easy")
	jevReject(t, `{"route":{"easy":"fake/m1","hard":3}}`, "jev.route.hard")
	jevReject(t, `{"route":"fake/m1"}`, "jev.route", "fake/m1")
}

func TestJevConfigInvalid(t *testing.T) {
	jevReject(t, `"on"`, "jev")
	jevReject(t, `[1]`, "jev")
	jevReject(t, `{"baseURL":"ftp://h.example/x"}`, "jev.baseURL", "h.example")
	jevReject(t, `{"baseURL":"/relative/only"}`, "jev.baseURL", "relative")
	jevReject(t, `{"baseURL":"https://"}`, "jev.baseURL")
	jevReject(t, `{"baseURL":"{env:TYPESAFE_API_KEY}"}`, "jev.baseURL")
	jevReject(t, `{"baseURL":7}`, "jev.baseURL")
	jevReject(t, `{"apiKey":true}`, "jev.apiKey")
	jevReject(t, `{"model":5}`, "jev.model")
	jevReject(t, `{"min_confidence":1.5}`, "jev.min_confidence")
	jevReject(t, `{"min_confidence":-0.1}`, "jev.min_confidence")
	jevReject(t, `{"min_confidence":"high"}`, "jev.min_confidence", "high")
}

func TestJevAllowedBaseURLs(t *testing.T) {
	cfg, err := loadJev(t, "", "")
	if err != nil {
		t.Fatal(err)
	}
	if got, want := cfg.AllowedBaseURLs(), cfg.BaseURLs(); !slices.Equal(got, want) || len(got) != 1 {
		t.Fatalf("without jev: %q, want %q", got, want)
	}

	cfg, err = loadJev(t, `{"baseURL":"http://127.0.0.1:7"}`, "")
	if err != nil {
		t.Fatal(err)
	}
	if got := cfg.AllowedBaseURLs(); !slices.Equal(got, []string{"http://127.0.0.1:7", "http://127.0.0.1:9/v1"}) {
		t.Fatalf("with jev: %q", got)
	}
	if got := cfg.BaseURLs(); !slices.Equal(got, []string{"http://127.0.0.1:9/v1"}) {
		t.Fatalf("BaseURLs changed: %q", got)
	}

	// A jev baseURL equal to a provider base URL is listed once.
	cfg, err = loadJev(t, `{"baseURL":"http://127.0.0.1:9/v1"}`, "")
	if err != nil {
		t.Fatal(err)
	}
	if got := cfg.AllowedBaseURLs(); !slices.Equal(got, []string{"http://127.0.0.1:9/v1"}) {
		t.Fatalf("dedup: %q", got)
	}
}
