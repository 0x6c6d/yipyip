package tui

import (
	"strings"
	"sync"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
	"github.com/muesli/termenv"

	"yipyip/internal/agent"
	"yipyip/internal/provider/providertest"
)

func plainView(m Model) string { return ansi.Strip(m.View()) }

// waitRequests polls until the fake provider has seen n requests.
func waitRequests(t *testing.T, srv *providertest.Server, n int) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for len(srv.Requests()) < n {
		if time.Now().After(deadline) {
			t.Fatalf("provider saw %d requests, want %d", len(srv.Requests()), n)
		}
		time.Sleep(time.Millisecond)
	}
}

func cancelTurn(t *testing.T, m Model, sess *agent.Session) Model {
	t.Helper()
	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	waitSession(t, sess)
	return next.(Model)
}

func slowResponse() providertest.Response {
	return providertest.Response{Text: "late", Delay: 30 * time.Second}
}

func TestThinkingLine(t *testing.T) {
	srv := providertest.New(slowResponse())
	defer srv.Close()
	m, sess, clock := newClockModel(t, srv)
	m, cmd := m.Command("hi")
	if cmd == nil {
		t.Fatal("Command returned no tick command")
	}
	if v := plainView(m); !strings.Contains(v, "✻ Thinking… (0s · esc to interrupt)") {
		t.Fatalf("view lacks the thinking line:\n%s", v)
	}
	clock.Advance(75 * time.Second)
	next, _ := m.Update(tickMsg(clock.Now()))
	m = next.(Model)
	if v := plainView(m); !strings.Contains(v, "(1m 15s · esc to interrupt)") {
		t.Fatalf("view lacks the elapsed time:\n%s", v)
	}
	// The line sits directly above the input box and keeps the height.
	lines := strings.Split(plainView(m), "\n")
	if len(lines) != 40 {
		t.Errorf("view has %d lines, want 40", len(lines))
	}
	for i, l := range lines {
		if strings.Contains(l, "✻ Thinking…") {
			if i+1 >= len(lines) || !strings.HasPrefix(lines[i+1], "╭") {
				t.Errorf("line below the thinking line = %q, want the input box", lines[i+1])
			}
		}
	}
	cancelTurn(t, m, sess)
}

func TestThinkingTokens(t *testing.T) {
	srv := providertest.New(
		providertest.Response{
			ToolCalls: []providertest.ToolCall{{ID: "c1", Name: "glob", Arguments: `{"pattern":"*"}`}},
			Usage:     &providertest.Usage{Prompt: 10, Completion: 1234},
		},
		slowResponse(),
	)
	defer srv.Close()
	m, sess, _ := newClockModel(t, srv)
	m, _ = m.Command("hi")
	waitRequests(t, srv, 2)
	m = m.Drain()
	if v := plainView(m); !strings.Contains(v, "↓ 1.2k tokens · esc to interrupt") {
		t.Fatalf("view lacks the token count:\n%s", v)
	}
	cancelTurn(t, m, sess)
}

func TestThinkingEarlierTurnNotCounted(t *testing.T) {
	srv := providertest.New(
		providertest.Response{Text: "done", Usage: &providertest.Usage{Prompt: 10, Completion: 500}},
		slowResponse(),
	)
	defer srv.Close()
	m, sess, _ := newClockModel(t, srv)
	m, _ = m.Command("first")
	waitSession(t, sess)
	m = m.Drain()
	if sess.Tokens().Out != 500 {
		t.Fatalf("out tokens = %d, want 500", sess.Tokens().Out)
	}
	m, _ = m.Command("second")
	if v := plainView(m); !strings.Contains(v, "✻ Thinking…") || strings.Contains(v, "↓") {
		t.Fatalf("second turn line must have no token count:\n%s", v)
	}
	cancelTurn(t, m, sess)
}

func TestThinkingWorkingVerb(t *testing.T) {
	srv := providertest.New(slowResponse())
	defer srv.Close()
	m, sess, _ := newClockModel(t, srv)
	m, _ = m.Command("hi")
	m.apply(agent.Event{Kind: agent.EvToolCall, AgentID: agent.MainAgentID, Tool: "glob", Args: `{"pattern":"*"}`})
	if v := plainView(m); !strings.Contains(v, "✻ Working…") || strings.Contains(v, "Thinking…") {
		t.Fatalf("want Working…:\n%s", v)
	}
	// A subagent's call does not count.
	m.apply(agent.Event{Kind: agent.EvToolCall, AgentID: "a1", Tool: "glob", Args: `{"pattern":"*"}`})
	m.apply(agent.Event{Kind: agent.EvToolResult, AgentID: agent.MainAgentID, Tool: "glob", Text: "x"})
	if v := plainView(m); !strings.Contains(v, "✻ Thinking…") || strings.Contains(v, "Working…") {
		t.Fatalf("want Thinking… again:\n%s", v)
	}
	cancelTurn(t, m, sess)
}

func TestThinkingColors(t *testing.T) {
	withColorProfile(t, termenv.TrueColor)
	srv := providertest.New(slowResponse())
	defer srv.Close()
	m, sess, _ := newClockModel(t, srv)
	m, _ = m.Command("hi")
	v := m.View()
	for _, want := range []string{"38;2;215;119;87", "38;5;244"} {
		if !strings.Contains(v, want) {
			t.Errorf("view lacks %q", want)
		}
	}
	cancelTurn(t, m, sess)
}

func TestThinkingGoneAfterTurn(t *testing.T) {
	srv := providertest.New(providertest.Response{Text: "hello"})
	defer srv.Close()
	m, sess, _ := newClockModel(t, srv)
	m, _ = m.Command("hi")
	waitSession(t, sess)
	m = m.Drain()
	if v := plainView(m); strings.Contains(v, "Thinking…") || strings.Contains(v, "Working…") {
		t.Fatalf("busy line still shown after the turn:\n%s", v)
	}
}

func TestThinkingNoTickWhileIdle(t *testing.T) {
	srv := providertest.New(slowResponse())
	defer srv.Close()
	m, sess, clock := newClockModel(t, srv)
	if _, cmd := m.Update(tickMsg(clock.Now())); cmd != nil {
		t.Fatal("idle model scheduled a tick")
	}
	m, _ = m.Command("hi")
	if _, cmd := m.Update(tickMsg(clock.Now())); cmd == nil {
		t.Fatal("busy model did not schedule the next tick")
	}
	m = cancelTurn(t, m, sess)
	m = m.Drain()
	if _, cmd := m.Update(tickMsg(clock.Now())); cmd != nil {
		t.Fatal("model scheduled a tick after the turn")
	}
}

func TestThinkingFormat(t *testing.T) {
	for _, c := range []struct {
		d    time.Duration
		want string
	}{
		{0, "0s"}, {400 * time.Millisecond, "0s"}, {59 * time.Second, "59s"},
		{60 * time.Second, "1m 00s"}, {75 * time.Second, "1m 15s"}, {65 * time.Second, "1m 05s"},
		{12 * time.Minute, "12m 00s"},
	} {
		if got := formatElapsed(c.d); got != c.want {
			t.Errorf("formatElapsed(%v) = %q, want %q", c.d, got, c.want)
		}
	}
	for _, c := range []struct {
		n    int
		want string
	}{{0, "0"}, {999, "999"}, {1000, "1.0k"}, {1234, "1.2k"}, {1500000, "1.5M"}} {
		if got := formatTokens(c.n); got != c.want {
			t.Errorf("formatTokens(%d) = %q, want %q", c.n, got, c.want)
		}
	}
}

// heldServer returns a fake provider whose response waits until release is
// closed. The channel is closed in t.Cleanup before the server is closed.
func heldServer(t *testing.T) (*providertest.Server, func()) {
	t.Helper()
	release := make(chan struct{})
	srv := providertest.NewFunc(func(n int, req providertest.Request) providertest.Response {
		<-release
		return providertest.Response{Text: "ok"}
	})
	t.Cleanup(srv.Close)
	var once sync.Once
	rel := func() { once.Do(func() { close(release) }) }
	t.Cleanup(rel)
	return srv, rel
}

func lastBlockText(m Model) string {
	if len(m.blocks) == 0 {
		return ""
	}
	return ansi.Strip(m.renderBlock(m.blocks[len(m.blocks)-1], 80))
}

func markerLines(m Model) []string {
	var out []string
	for _, l := range strings.Split(plainView(m), "\n") {
		if l = strings.TrimSpace(l); strings.HasPrefix(l, "✻ Worked for") || strings.HasPrefix(l, "✻ Interrupted after") {
			out = append(out, l)
		}
	}
	return out
}

// finishHeld runs one turn held open while the clock advances by d.
func finishHeld(t *testing.T, m Model, sess *agent.Session, clock *fakeClock, release func(), d time.Duration) Model {
	t.Helper()
	m, _ = m.Command("hi")
	waitBusy(t, sess)
	clock.Advance(d)
	release()
	waitSession(t, sess)
	return m.Drain()
}

func TestFinishedWorkedFor(t *testing.T) {
	srv, release := heldServer(t)
	m, sess, clock := newClockModel(t, srv)
	m = finishHeld(t, m, sess, clock, release, 75*time.Second)
	if got := lastBlockText(m); got != "✻ Worked for 1m 15s" {
		t.Fatalf("last block = %q", got)
	}
	if v := plainView(m); strings.Contains(v, "Thinking…") {
		t.Fatalf("view still has Thinking…:\n%s", v)
	}
	// The text is fixed: later clock movement does not change it.
	clock.Advance(time.Hour)
	if got := lastBlockText(m); got != "✻ Worked for 1m 15s" {
		t.Fatalf("marker changed: %q", got)
	}
}

func TestFinishedUnderOneSecond(t *testing.T) {
	srv, release := heldServer(t)
	m, sess, clock := newClockModel(t, srv)
	m = finishHeld(t, m, sess, clock, release, 400*time.Millisecond)
	if got := lastBlockText(m); got != "✻ Worked for 0s" {
		t.Fatalf("last block = %q", got)
	}
}

func TestFinishedInterrupted(t *testing.T) {
	srv := providertest.New(slowResponse())
	defer srv.Close()
	m, sess, clock := newClockModel(t, srv)
	m, _ = m.Command("hi")
	waitBusy(t, sess)
	clock.Advance(12 * time.Second)
	m = cancelTurn(t, m, sess)
	m = m.Drain()
	if got := lastBlockText(m); got != "✻ Interrupted after 12s" {
		t.Fatalf("last block = %q", got)
	}
	if n := len(markerLines(m)); n != 1 {
		t.Fatalf("%d marker lines, want 1", n)
	}
}

func TestFinishedOnePerTurn(t *testing.T) {
	srv, release := heldServer(t)
	release() // responses return at once; the clock still moves while busy
	m, sess, clock := newClockModel(t, srv)
	m = finishHeld(t, m, sess, clock, release, 5*time.Second)
	m = finishHeld(t, m, sess, clock, release, 7*time.Second)
	lines := markerLines(m)
	if len(lines) != 2 || lines[0] != "✻ Worked for 5s" || lines[1] != "✻ Worked for 7s" {
		t.Fatalf("marker lines = %q", lines)
	}
}

func TestFinishedNotForSlash(t *testing.T) {
	srv := providertest.New(slowResponse())
	defer srv.Close()
	m, _, _ := newClockModel(t, srv)
	m, _ = m.Command("/mode auto")
	m, _ = m.Command("/effort low")
	for _, b := range m.blocks {
		if b.kind == blockMarker {
			t.Fatalf("unexpected marker %q", b.text)
		}
	}
	// A turn-done event with no running turn adds nothing either.
	m.apply(agent.Event{Kind: agent.EvTurnDone, AgentID: agent.MainAgentID})
	if n := len(markerLines(m)); n != 0 {
		t.Fatalf("%d marker lines, want 0", n)
	}
}

func TestFinishedColors(t *testing.T) {
	withColorProfile(t, termenv.TrueColor)
	srv, release := heldServer(t)
	m, sess, clock := newClockModel(t, srv)
	m = finishHeld(t, m, sess, clock, release, 3*time.Second)
	for _, l := range strings.Split(m.View(), "\n") {
		if strings.Contains(ansi.Strip(l), "✻ Worked for") {
			for _, want := range []string{"38;2;215;119;87", "38;5;244"} {
				if !strings.Contains(l, want) {
					t.Errorf("marker line lacks %q: %q", want, l)
				}
			}
			return
		}
	}
	t.Fatal("no marker line in view")
}

func TestFinishedClearedByNew(t *testing.T) {
	srv, release := heldServer(t)
	m, sess, clock := newClockModel(t, srv)
	m = finishHeld(t, m, sess, clock, release, 3*time.Second)
	if len(markerLines(m)) != 1 {
		t.Fatal("no marker before /new")
	}
	m, _ = m.Command("/new")
	if n := len(markerLines(m)); n != 0 {
		t.Fatalf("%d marker lines after /new", n)
	}
	for _, b := range m.blocks {
		if b.kind == blockMarker {
			t.Fatal("marker block survived /new")
		}
	}
}
