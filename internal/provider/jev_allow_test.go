package provider_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"yipyip/internal/config"
	"yipyip/internal/provider"
	"yipyip/internal/provider/providertest"
)

// allowConfig loads a real config with one fake provider at providerURL and
// the given "jev" value (omitted when jev is ""). TYPESAFE_API_KEY resolves
// to SECRET123.
func allowConfig(t *testing.T, providerURL, jev string) *config.Config {
	t.Helper()
	home, dir := t.TempDir(), t.TempDir()
	if err := os.MkdirAll(filepath.Join(home, ".config", "yipyip"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(config.GlobalPath(home), []byte(providertest.ConfigJSON(providerURL, "k", 8000)), 0o644); err != nil {
		t.Fatal(err)
	}
	if jev != "" {
		if err := os.WriteFile(filepath.Join(dir, config.ProjectFile), []byte(`{"jev":`+jev+`}`), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	cfg, err := config.Load(home, dir, func(k string) string {
		if k == "TYPESAFE_API_KEY" {
			return "SECRET123"
		}
		return ""
	})
	if err != nil {
		t.Fatal(err)
	}
	return cfg
}

// askVia asks target once through a JevClient on the transport built from
// cfg.AllowedBaseURLs().
func askVia(t *testing.T, cfg *config.Config, target string) error {
	t.Helper()
	hc, err := provider.NewHTTPClient(cfg.AllowedBaseURLs())
	if err != nil {
		t.Fatal(err)
	}
	_, _, err = provider.NewJevClient(hc, target, "SECRET123", "jev-1").Ask(context.Background(), "s", jevQuestions())
	return err
}

func TestJevHostRefusedUnset(t *testing.T) {
	srv := providertest.New()
	defer srv.Close()
	jev := jevServe(providertest.JevResponse{Answers: jevAnswers()})
	defer jev.Close()
	cfg := allowConfig(t, srv.URL, "")
	err := askVia(t, cfg, jev.URL)
	if !errors.Is(err, provider.ErrHostNotAllowed) {
		t.Fatalf("want ErrHostNotAllowed, got %v", err)
	}
	if n := len(jev.Requests()); n != 0 {
		t.Fatalf("%d requests reached jev without jev configured", n)
	}
}

func TestJevHostAllowedSet(t *testing.T) {
	srv := providertest.New()
	defer srv.Close()
	jev := jevServe(providertest.JevResponse{Answers: jevAnswers()})
	defer jev.Close()
	cfg := allowConfig(t, srv.URL, `{"baseURL":"`+jev.URL+`","apiKey":"{env:TYPESAFE_API_KEY}"}`)
	if err := askVia(t, cfg, jev.URL); err != nil {
		t.Fatalf("Ask with jev configured: %v", err)
	}
	if n := len(jev.Requests()); n != 1 {
		t.Fatalf("%d requests", n)
	}
}

func TestTransportJevOnlyItsHost(t *testing.T) {
	srv := providertest.New()
	defer srv.Close()
	jev := jevServe(providertest.JevResponse{Answers: jevAnswers()})
	defer jev.Close()
	other := jevServe(providertest.JevResponse{Answers: jevAnswers()})
	defer other.Close()
	cfg := allowConfig(t, srv.URL, `{"baseURL":"`+jev.URL+`"}`)
	err := askVia(t, cfg, other.URL)
	if !errors.Is(err, provider.ErrHostNotAllowed) {
		t.Fatalf("want ErrHostNotAllowed for a third port, got %v", err)
	}
	if n := len(other.Requests()); n != 0 {
		t.Fatalf("%d requests reached the third port", n)
	}
}
