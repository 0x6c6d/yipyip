package provider_test

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"yipyip/internal/config"
	"yipyip/internal/provider"
	"yipyip/internal/provider/providertest"
)

// countingServer starts an httptest server on 127.0.0.1 that counts its
// requests and answers with h (or "ok" when h is nil).
func countingServer(t *testing.T, h http.HandlerFunc) (*httptest.Server, *atomic.Int64) {
	t.Helper()
	n := new(atomic.Int64)
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n.Add(1)
		if h != nil {
			h(w, r)
			return
		}
		_, _ = w.Write([]byte("ok"))
	}))
	t.Cleanup(s.Close)
	return s, n
}

func searchGet(c *http.Client, u string) error {
	resp, err := c.Get(u)
	if err != nil {
		return err
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return errors.New(resp.Status)
	}
	return nil
}

// searchConfig loads a real config with a provider and a search block.
func searchConfig(t *testing.T, providerURL, searchURL string) *config.Config {
	t.Helper()
	home, dir := t.TempDir(), t.TempDir()
	if err := os.MkdirAll(filepath.Dir(config.GlobalPath(home)), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(config.GlobalPath(home), []byte(providertest.ConfigJSON(providerURL+"/v1", "k", 8000)), 0o644); err != nil {
		t.Fatal(err)
	}
	project := `{"search":{"baseURL":"` + searchURL + `","apiKey":"k2"}}`
	if err := os.WriteFile(filepath.Join(dir, config.ProjectFile), []byte(project), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Load(home, dir, func(string) string { return "" })
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Search == nil || cfg.Search.BaseURL != searchURL {
		t.Fatalf("search block not loaded: %+v", cfg.Search)
	}
	return cfg
}

func TestSearchTransportReachesSearchHost(t *testing.T) {
	s, n := countingServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/hop" {
			http.Redirect(w, r, "/res/v1/web/search?q=y", http.StatusFound)
			return
		}
		_, _ = w.Write([]byte("ok"))
	})
	c, err := provider.NewSearchHTTPClient(s.URL)
	if err != nil || c == nil {
		t.Fatalf("NewSearchHTTPClient: %v", err)
	}
	if c.Timeout != provider.SearchTimeout {
		t.Fatalf("Timeout = %v, want %v", c.Timeout, provider.SearchTimeout)
	}
	if _, ok := c.Transport.(*provider.Transport); !ok {
		t.Fatalf("Transport is %T", c.Transport)
	}
	if err := searchGet(c, s.URL+"/res/v1/web/search?q=x"); err != nil {
		t.Fatalf("search GET: %v", err)
	}
	if err := searchGet(c, s.URL+"/hop"); err != nil {
		t.Fatalf("same-host redirect: %v", err)
	}
	if got := n.Load(); got != 3 {
		t.Fatalf("search server got %d requests, want 3", got)
	}
}

func TestSearchTransportRefusesProviderHost(t *testing.T) {
	prov, pn := countingServer(t, nil)
	s, _ := countingServer(t, nil)
	c, err := provider.NewSearchHTTPClient(s.URL)
	if err != nil {
		t.Fatal(err)
	}
	u := prov.URL + "/v1/chat/completions"
	if err := searchGet(c, u); !errors.Is(err, provider.ErrHostNotAllowed) {
		t.Fatalf("%s: want ErrHostNotAllowed, got %v", u, err)
	}
	if got := pn.Load(); got != 0 {
		t.Fatalf("refused provider host got %d requests", got)
	}
}

func TestSearchHostRefusedForProviderClient(t *testing.T) {
	prov, _ := countingServer(t, nil)
	s, sn := countingServer(t, nil)
	cfg := searchConfig(t, prov.URL, s.URL)
	hc, err := provider.NewHTTPClient(cfg.BaseURLs())
	if err != nil {
		t.Fatal(err)
	}
	if err := searchGet(hc, s.URL+"/res/v1/web/search?q=x"); !errors.Is(err, provider.ErrHostNotAllowed) {
		t.Fatalf("want ErrHostNotAllowed, got %v", err)
	}
	if got := sn.Load(); got != 0 {
		t.Fatalf("search server got %d requests", got)
	}
}

func TestSearchRedirectRefused(t *testing.T) {
	other, on := countingServer(t, nil)
	s, _ := countingServer(t, func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, other.URL+"/x", http.StatusFound)
	})
	c, err := provider.NewSearchHTTPClient(s.URL)
	if err != nil {
		t.Fatal(err)
	}
	if err := searchGet(c, s.URL+"/res/v1/web/search?q=x"); !errors.Is(err, provider.ErrHostNotAllowed) {
		t.Fatalf("want ErrHostNotAllowed, got %v", err)
	}
	if got := on.Load(); got != 0 {
		t.Fatalf("redirect target got %d requests", got)
	}
}

func TestSearchTransportInvalidBaseURL(t *testing.T) {
	for _, bad := range []string{"", "ftp://127.0.0.1", "http://", "127.0.0.1:80"} {
		c, err := provider.NewSearchHTTPClient(bad)
		if err == nil || c != nil {
			t.Fatalf("%q: client %v, err %v", bad, c, err)
		}
		if bad != "" && strings.Contains(err.Error(), bad) {
			t.Fatalf("%q: error contains the URL: %v", bad, err)
		}
	}
}
