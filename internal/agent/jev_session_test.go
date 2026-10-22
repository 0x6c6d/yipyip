package agent

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"yipyip/internal/config"
	"yipyip/internal/provider"
	"yipyip/internal/provider/providertest"
)

// newJevSession builds a session on the fake provider srv with the given
// "jev" value in the project file (none when jev is ""). TYPESAFE_API_KEY
// resolves to a fake jev key.
func newJevSession(t *testing.T, srv *providertest.Server, jev string) *Session {
	t.Helper()
	base, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	home, dir := filepath.Join(base, "home"), filepath.Join(base, "start")
	for _, d := range []string{filepath.Join(home, ".config", "yipyip"), dir} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(config.GlobalPath(home), []byte(providertest.ConfigJSON(srv.URL, "{env:FAKE_KEY}", 8000)), 0o644); err != nil {
		t.Fatal(err)
	}
	if jev != "" {
		if err := os.WriteFile(filepath.Join(dir, config.ProjectFile), []byte(`{"jev":`+jev+`}`), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	cfg, err := config.Load(home, dir, func(k string) string {
		switch k {
		case "FAKE_KEY":
			return "SECRET123"
		case "TYPESAFE_API_KEY":
			return "jev-session-key"
		}
		return ""
	})
	if err != nil {
		t.Fatal(err)
	}
	s, err := NewSession(Options{Config: cfg, StartDir: dir})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Cancel(); s.Wait() })
	return s
}

func TestJevSessionClient(t *testing.T) {
	srv := providertest.New(fakeResp{Text: "chat ok"})
	defer srv.Close()
	jev := providertest.NewJev(func(int, providertest.JevRequest) providertest.JevResponse {
		return providertest.JevResponse{Answers: map[string]any{"r": providertest.JevNoulAnswer(0.3)}}
	})
	defer jev.Close()
	qs := []provider.JevQuestion{{Name: "r", Type: provider.JevNoul, Instructions: "risky?"}}

	s := newJevSession(t, srv, `{"baseURL":"`+jev.URL+`","apiKey":"{env:TYPESAFE_API_KEY}","model":"jx"}`)
	c := s.JevClient()
	if c == nil {
		t.Fatal("JevClient nil with jev and apiKey configured")
	}
	if s.JevClient() != c {
		t.Fatal("JevClient not built once")
	}
	ans, _, err := c.Ask(context.Background(), "state", qs)
	if err != nil || len(ans) != 1 || ans[0].Noul != 0.3 {
		t.Fatalf("Ask: %+v, %v", ans, err)
	}
	reqs := jev.Requests()
	if len(reqs) != 1 || reqs[0].Model != "jx" || reqs[0].Header.Get("Authorization") != "Bearer jev-session-key" {
		t.Fatalf("jev requests: %+v", reqs)
	}

	// The provider chat still goes through the same client.
	evs := submitAndWait(t, s, "hi")
	if !findEvent(evs, EvTurnDone, "") || len(srv.Requests()) != 1 {
		t.Fatalf("chat did not reach the provider: %+v", evs)
	}

	if newJevSession(t, srv, "").JevClient() != nil {
		t.Fatal("JevClient without jev")
	}
	if newJevSession(t, srv, `{"baseURL":"`+jev.URL+`"}`).JevClient() != nil {
		t.Fatal("JevClient without apiKey")
	}
}
