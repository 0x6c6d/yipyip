package tui

import (
	"strings"
	"testing"

	"yipyip/internal/provider/providertest"
)

func TestCompactWithoutSession(t *testing.T) {
	m := New(Options{StartDir: "/w"})
	m, cmd := m.Command("/compact")
	if cmd != nil {
		t.Fatal("/compact without a session returned a command")
	}
	if v := m.View(); !strings.Contains(v, "nothing to compact") {
		t.Fatalf("view:\n%s", v)
	}
}

func TestCompactNothingToCompact(t *testing.T) {
	srv := providertest.New()
	t.Cleanup(srv.Close)
	m, _ := newSessionModel(t, srv)
	m, cmd := m.Command("/compact")
	if cmd == nil {
		t.Fatal("no command")
	}
	if v := m.View(); !strings.Contains(v, "compacting...") {
		t.Fatalf("no compacting line:\n%s", v)
	}
	next, _ := m.Update(cmd())
	v := next.(Model).View()
	if !strings.Contains(v, "nothing to compact") {
		t.Fatalf("view:\n%s", v)
	}
	if n := len(srv.Requests()); n != 0 {
		t.Fatalf("%d provider requests", n)
	}
}

func TestCompactRunsAndReports(t *testing.T) {
	srv := providertest.New(
		providertest.Response{Text: "a1"}, providertest.Response{Text: "a2"},
		providertest.Response{Text: "a3"}, providertest.Response{Text: "the summary"})
	t.Cleanup(srv.Close)
	m, sess := newSessionModel(t, srv)
	for _, text := range []string{"q1", "q2", "q3"} {
		m, _ = m.Command(text)
		waitSession(t, sess)
	}
	m = m.Drain()
	m, cmd := m.Command("/compact")
	next, _ := m.Update(cmd())
	v := next.(Model).Drain().View()
	if !strings.Contains(v, "compacted:") || strings.Contains(v, "error:") {
		t.Fatalf("view:\n%s", v)
	}
}

func TestCompactErrorMessages(t *testing.T) {
	m := New(Options{StartDir: "/w"})
	next, _ := m.Update(compactDoneMsg{err: errFake("boom")})
	if v := next.(Model).View(); !strings.Contains(v, "error: boom") {
		t.Fatalf("view:\n%s", v)
	}
}

type errFake string

func (e errFake) Error() string { return string(e) }
