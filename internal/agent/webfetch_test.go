package agent

import (
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"yipyip/internal/config"
	"yipyip/internal/provider"
	"yipyip/internal/provider/providertest"
)

// wfPage starts a local text/plain page and counts the requests it gets.
func wfPage(t *testing.T) (*httptest.Server, *atomic.Int64) {
	t.Helper()
	var hits atomic.Int64
	pg := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits.Add(1)
		w.Header().Set("Content-Type", "text/plain")
		io.WriteString(w, "the magic word is plover")
	}))
	t.Cleanup(pg.Close)
	return pg, &hits
}

// wfClient is the fetch client allowed to reach only the page's address.
func wfClient(pg *httptest.Server) *http.Client {
	return provider.NewFetchHTTPClient(provider.FetchOptions{AllowAddr: pg.Listener.Addr().String(), Timeout: 5 * time.Second})
}

// wfFake scripts the provider: a user message "fetch" makes the main agent
// call web_fetch with target; "spawn" makes it spawn a subagent and wait for
// a1, and the subagent calls web_fetch with target. Anything else is a plain
// text reply.
func wfFake(target string) *providertest.Server {
	call := fakeCall{ID: "c1", Name: "web_fetch", Arguments: `{"url":"` + target + `"}`}
	return providertest.NewFunc(func(_ int, req providertest.Request) fakeResp {
		msgs := req.Messages
		if len(msgs) == 0 {
			return fakeResp{Text: "empty"}
		}
		last := msgs[len(msgs)-1]
		if strings.Contains(msgs[0].Content, "You are subagent") {
			if last.Role == "user" {
				sc := call
				sc.ID = "s1"
				return fakeResp{ToolCalls: []fakeCall{sc}}
			}
			return fakeResp{Text: "sub done"}
		}
		if last.Role == "user" {
			switch {
			case strings.HasPrefix(last.Content, "fetch"):
				return fakeResp{ToolCalls: []fakeCall{call}}
			case strings.HasPrefix(last.Content, "spawn"):
				return fakeResp{ToolCalls: []fakeCall{
					{ID: "c1", Name: "spawn_agent", Arguments: `{"task":"read the page"}`},
					{ID: "c2", Name: "wait_agent", Arguments: `{"id":"a1"}`},
				}}
			}
		}
		return fakeResp{Text: "done"}
	})
}

// wfSession builds a session (no search block) for srv with the given fetch
// client (nil = the tools default fetch client).
func wfSession(t *testing.T, srv *providertest.Server, fetch *http.Client) *Session {
	t.Helper()
	base, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	home, dir := filepath.Join(base, "home"), filepath.Join(base, "start")
	for _, d := range []string{filepath.Dir(config.GlobalPath(home)), dir} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(config.GlobalPath(home), []byte(providertest.ConfigJSON(srv.URL, "{env:FAKE_KEY}", 8000)), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Load(home, dir, func(k string) string {
		if k == "FAKE_KEY" {
			return secretValue
		}
		return ""
	})
	if err != nil {
		t.Fatalf("config.Load: %v", err)
	}
	s, err := NewSession(Options{Config: cfg, StartDir: dir, FetchClient: fetch})
	if err != nil {
		t.Fatalf("NewSession: %v", err)
	}
	t.Cleanup(func() {
		s.Cancel()
		waitTurn(t, s, 5*time.Second)
	})
	return s
}

// wfResults returns the web_fetch tool result events of evs.
func wfResults(evs []Event) []Event {
	var out []Event
	for _, ev := range evs {
		if ev.Kind == EvToolResult && ev.Tool == "web_fetch" {
			out = append(out, ev)
		}
	}
	return out
}

// wfApprovals returns the approval events for tool.
func wfApprovals(evs []Event, tool string) []Event {
	var out []Event
	for _, ev := range evs {
		if ev.Tool == tool {
			out = append(out, ev)
		}
	}
	return out
}

func TestWebFetchGated(t *testing.T) {
	t.Run("manual main deny", func(t *testing.T) {
		pg, hits := wfPage(t)
		target := pg.URL + "/doc"
		srv := wfFake(target)
		defer srv.Close()
		s := wfSession(t, srv, wfClient(pg))
		s.SetMode(ModeManual)
		approvals, all := wsTurn(t, s, "fetch it", wsAll(Deny))
		if len(approvals) != 1 || approvals[0].Tool != "web_fetch" || approvals[0].AgentID != MainAgentID ||
			!strings.Contains(approvals[0].Args, target) {
			t.Fatalf("approvals: %+v", approvals)
		}
		if n := hits.Load(); n != 0 {
			t.Fatalf("page got %d requests after deny", n)
		}
		res := wfResults(all)
		if len(res) != 1 || !res[0].IsError || !strings.Contains(res[0].Text, DeniedMessage) {
			t.Fatalf("web_fetch results: %+v", res)
		}
	})

	t.Run("manual main allow", func(t *testing.T) {
		pg, hits := wfPage(t)
		target := pg.URL + "/doc"
		srv := wfFake(target)
		defer srv.Close()
		s := wfSession(t, srv, wfClient(pg))
		s.SetMode(ModeManual)
		approvals, all := wsTurn(t, s, "fetch it", wsAll(Allow))
		if len(approvals) != 1 || approvals[0].Tool != "web_fetch" || !strings.Contains(approvals[0].Args, target) {
			t.Fatalf("approvals: %+v", approvals)
		}
		if n := hits.Load(); n != 1 {
			t.Fatalf("page requests: %d, want 1", n)
		}
		res := wfResults(all)
		if len(res) != 1 || res[0].IsError || !strings.HasPrefix(res[0].Text, "url: ") || !strings.Contains(res[0].Text, "plover") {
			t.Fatalf("web_fetch results: %+v", res)
		}
	})

	t.Run("manual subagent deny", func(t *testing.T) {
		pg, hits := wfPage(t)
		target := pg.URL + "/doc"
		srv := wfFake(target)
		defer srv.Close()
		s := wfSession(t, srv, wfClient(pg))
		s.SetMode(ModeManual)
		approvals, all := wsTurn(t, s, "spawn a helper", func(_, tool string) Decision {
			if tool == "spawn_agent" {
				return Allow
			}
			return Deny
		})
		if sp := wfApprovals(approvals, "spawn_agent"); len(sp) != 1 {
			t.Fatalf("spawn_agent approvals: %+v", approvals)
		}
		fa := wfApprovals(approvals, "web_fetch")
		if len(fa) != 1 || fa[0].AgentID != "a1" || !strings.Contains(fa[0].Args, target) {
			t.Fatalf("subagent web_fetch approvals: %+v", fa)
		}
		if n := hits.Load(); n != 0 {
			t.Fatalf("page got %d requests after subagent deny", n)
		}
		res := wfResults(all)
		if len(res) != 1 || res[0].AgentID != "a1" || !res[0].IsError || !strings.Contains(res[0].Text, DeniedMessage) {
			t.Fatalf("subagent web_fetch results: %+v", res)
		}
	})

	t.Run("auto", func(t *testing.T) {
		pg, hits := wfPage(t)
		srv := wfFake(pg.URL + "/doc")
		defer srv.Close()
		s := wfSession(t, srv, wfClient(pg))
		s.SetMode(ModeAuto)
		approvals, all := wsTurn(t, s, "fetch it", wsAll(Deny))
		if len(approvals) != 0 {
			t.Fatalf("auto mode prompted: %+v", approvals)
		}
		if n := hits.Load(); n != 1 {
			t.Fatalf("page requests: %d, want 1", n)
		}
		res := wfResults(all)
		if len(res) != 1 || res[0].IsError || !strings.HasPrefix(res[0].Text, "url: ") {
			t.Fatalf("web_fetch results: %+v", res)
		}
	})

	// "always" for web_fetch covers only web_fetch.
	for _, tc := range []struct {
		tool  string
		gated bool
	}{
		{"bash", true}, {"write_file", true}, {"edit_file", true}, {"spawn_agent", true}, {"web_search", true},
		{"read_file", false}, {"glob", false}, {"grep", false}, {"question", false},
	} {
		t.Run("always web_fetch vs "+tc.tool, func(t *testing.T) {
			g, reqs := recordingGate(func(r *ApprovalRequest) Decision {
				if r.Tool == "web_fetch" {
					return AllowAlways
				}
				return Deny
			})
			if !mustCheck(t, g, ModeManual, "web_fetch") || !mustCheck(t, g, ModeManual, "web_fetch") {
				t.Fatal("web_fetch denied after AllowAlways")
			}
			if n := len(reqs()); n != 1 {
				t.Fatalf("web_fetch prompts = %d, want 1", n)
			}
			if got := IsGated(tc.tool); got != tc.gated {
				t.Fatalf("IsGated(%q) = %v, want %v", tc.tool, got, tc.gated)
			}
			ok := mustCheck(t, g, ModeManual, tc.tool)
			got := reqs()
			if tc.gated {
				if ok {
					t.Fatalf("always for web_fetch let %s through", tc.tool)
				}
				if len(got) != 2 || got[1].Tool != tc.tool {
					t.Fatalf("%s was not prompted after always for web_fetch: %d prompts", tc.tool, len(got))
				}
			} else if !ok || len(got) != 1 {
				t.Fatalf("ungated %s: ok %v, prompts %d", tc.tool, ok, len(got))
			}
		})
	}
}

func TestWebFetchRegistered(t *testing.T) {
	t.Run("offered to main agent and subagent", func(t *testing.T) {
		pg, _ := wfPage(t)
		srv := wfFake(pg.URL + "/doc")
		defer srv.Close()
		s := wfSession(t, srv, wfClient(pg))
		s.SetMode(ModeManual)
		wsTurn(t, s, "spawn a helper", func(_, tool string) Decision {
			if tool == "spawn_agent" {
				return Allow
			}
			return Deny
		})
		var mainFirst, subFirst *providertest.Request
		reqs := srv.Requests()
		for i := range reqs {
			r := &reqs[i]
			if len(r.Messages) == 0 {
				continue
			}
			if strings.Contains(r.Messages[0].Content, "You are subagent") {
				if subFirst == nil {
					subFirst = r
				}
			} else if mainFirst == nil {
				mainFirst = r
			}
		}
		if mainFirst == nil || subFirst == nil {
			t.Fatalf("missing requests: main %v, subagent %v", mainFirst != nil, subFirst != nil)
		}
		for who, r := range map[string]*providertest.Request{"main": mainFirst, "subagent": subFirst} {
			if !wsHas(r.ToolNames, "web_fetch") {
				t.Errorf("%s: web_fetch not offered: %v", who, r.ToolNames)
			}
			if wsHas(r.ToolNames, "web_search") {
				t.Errorf("%s: web_search offered without a search block: %v", who, r.ToolNames)
			}
		}
	})

	t.Run("default client refuses loopback", func(t *testing.T) {
		pg, hits := wfPage(t)
		srv := wfFake(pg.URL + "/doc")
		defer srv.Close()
		s := wfSession(t, srv, nil)
		s.SetMode(ModeAuto)
		_, all := wsTurn(t, s, "fetch it", wsAll(Deny))
		if n := hits.Load(); n != 0 {
			t.Fatalf("page got %d requests through the default client", n)
		}
		res := wfResults(all)
		if len(res) != 1 || res[0].Text != "web_fetch failed: address not allowed" {
			t.Fatalf("web_fetch results: %+v", res)
		}
	})
}
