package agent

import (
	"strings"
	"testing"

	"yipyip/internal/provider/providertest"
)

func newAppendSession(t *testing.T, srv *providertest.Server, extra string) *Session {
	t.Helper()
	base, dir := newLoopSession(t, srv)
	if extra == "" {
		return base
	}
	s, err := NewSession(Options{Config: base.cfg, StartDir: dir, SystemAppend: extra})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Cancel(); waitTurn(t, s, 5e9) })
	return s
}

func TestSystemAppend(t *testing.T) {
	srv := providertest.New(fakeResp{Text: "ok"})
	defer srv.Close()
	s := newAppendSession(t, srv, "EXTRA-RULES-42")
	if !strings.HasSuffix(s.SystemPrompt(), "\n\nEXTRA-RULES-42") {
		t.Fatalf("prompt does not end with append")
	}
	submitAndWait(t, s, "hi")
	reqs := srv.Requests()
	if len(reqs) == 0 || len(reqs[0].Messages) == 0 {
		t.Fatal("no request")
	}
	m := reqs[0].Messages[0]
	if m.Role != "system" || !strings.Contains(m.Content, "EXTRA-RULES-42") {
		t.Fatalf("first message = %+v", m)
	}
}

func TestSystemAppendAfterReset(t *testing.T) {
	s := newAppendSession(t, nil, "EXTRA-RULES-42")
	s.Reset()
	if !strings.HasSuffix(s.SystemPrompt(), "\n\nEXTRA-RULES-42") {
		t.Fatal("append lost after Reset")
	}
}

func TestSystemAppendEmpty(t *testing.T) {
	a, dir := newLoopSession(t, nil)
	b, err := NewSession(Options{Config: a.cfg, StartDir: dir})
	if err != nil {
		t.Fatal(err)
	}
	if a.SystemPrompt() != b.SystemPrompt() || strings.HasSuffix(a.SystemPrompt(), "\n\n") {
		t.Fatal("prompts differ or trailing blank lines")
	}
}
