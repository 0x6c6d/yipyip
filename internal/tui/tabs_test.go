package tui

import (
	"errors"
	"os"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"yipyip/internal/agent"
	"yipyip/internal/provider/providertest"
)

// tabTurn types text into the current tab, presses Enter (so the input
// history records it), waits for the turn and applies its events.
func tabTurn(t *testing.T, m Model, text string) Model {
	t.Helper()
	sess := m.sess
	m.input.SetValue(text)
	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	waitSession(t, sess)
	return next.(Model).Drain()
}

// mustAddTab adds a tab or fails the test.
func mustAddTab(t *testing.T, m *Model) {
	t.Helper()
	if err := m.addTab(); err != nil {
		t.Fatalf("addTab: %v", err)
	}
}

// answerServer answers every request with text.
func answerServer(t *testing.T, text string) *providertest.Server {
	t.Helper()
	srv := providertest.NewFunc(func(int, providertest.Request) providertest.Response {
		return providertest.Response{Text: text, Usage: &providertest.Usage{Prompt: 10, Completion: 5}}
	})
	t.Cleanup(srv.Close)
	return srv
}

func TestAddTabSwitchesToNewTab(t *testing.T) {
	m, sess := newSessionModel(t, answerServer(t, "hello"))
	m, _ = m.Command("/effort high")
	m, _ = m.Command("/mode auto")
	m = tabTurn(t, m, "first question")

	mustAddTab(t, &m)
	if len(m.tabs) != 2 || m.cur != 1 {
		t.Fatalf("tabs = %d, cur = %d, want 2 and 1", len(m.tabs), m.cur)
	}
	if len(m.blocks) != 0 || len(m.queue) != 0 || m.turnActive {
		t.Fatalf("new tab not empty: %d blocks, queue %q, turnActive %v", len(m.blocks), m.queue, m.turnActive)
	}
	if m.sess == nil || m.sess == sess {
		t.Fatal("new tab has no session of its own")
	}
	if n := len(m.sess.History()); n != 1 {
		t.Errorf("new session history = %d messages, want 1", n)
	}
	if got := m.sess.ModelID(); got != sess.ModelID() {
		t.Errorf("model = %q, want %q", got, sess.ModelID())
	}
	if got := m.sess.Effort(); got != "high" {
		t.Errorf("effort = %q, want high", got)
	}
	if got := m.sess.Mode(); got != agent.ModeAuto {
		t.Errorf("mode = %q, want auto", got)
	}
	if v := belowTabBar(plainView(m)); strings.Contains(v, "first question") || strings.Contains(v, "hello") {
		t.Errorf("new tab shows the old conversation:\n%s", v)
	}
}

func TestSwitchTabSwapsState(t *testing.T) {
	m, sess := newSessionModel(t, answerServer(t, "the answer"))
	m = tabTurn(t, m, "question one")
	if v := plainView(m); !strings.Contains(v, "the answer") {
		t.Fatalf("no assistant block in tab 0:\n%s", v)
	}
	m.input.SetValue("draft zero")

	mustAddTab(t, &m)
	m.switchTab(0)
	m.switchTab(1)
	if m.cur != 1 {
		t.Fatalf("cur = %d, want 1", m.cur)
	}
	v := belowTabBar(plainView(m))
	if strings.Contains(v, "the answer") || strings.Contains(v, "question one") {
		t.Errorf("tab 1 shows tab 0's conversation:\n%s", v)
	}
	if got := m.input.Value(); got != "" {
		t.Errorf("tab 1 input = %q, want empty", got)
	}
	if _, got := histKey(m, tea.KeyUp); got != "" {
		t.Errorf("tab 1 Up = %q, want nothing", got)
	}
	if in, out, total := m.sess.TokenCounts(); in != 0 || out != 0 || total != 0 {
		t.Errorf("tab 1 tokens = %d/%d/%d, want 0", in, out, total)
	}
	if _, row2 := m.StatusRows(); !strings.Contains(row2, "total 0") {
		t.Errorf("tab 1 status row 2 = %q", row2)
	}

	m.switchTab(0)
	if m.cur != 0 || m.sess != sess {
		t.Fatalf("cur = %d, want 0 with the first session", m.cur)
	}
	if v := plainView(m); !strings.Contains(v, "the answer") || !strings.Contains(v, "question one") {
		t.Errorf("tab 0 conversation not back:\n%s", v)
	}
	if got := m.input.Value(); got != "draft zero" {
		t.Errorf("tab 0 draft = %q, want %q", got, "draft zero")
	}
	if _, out, total := m.sess.TokenCounts(); out == 0 || total == 0 {
		t.Errorf("tab 0 tokens lost: out %d total %d", out, total)
	}
	m.input.Reset()
	if _, got := histKey(m, tea.KeyUp); got != "question one" {
		t.Errorf("tab 0 Up = %q, want %q", got, "question one")
	}

	// Out of range: no change.
	m.switchTab(5)
	m.switchTab(-1)
	if m.cur != 0 || len(m.tabs) != 2 {
		t.Errorf("out-of-range switch changed state: cur %d, tabs %d", m.cur, len(m.tabs))
	}
}

func TestGlobalModelSharedAcrossTabs(t *testing.T) {
	m, sess := newSessionModel(t, answerServer(t, "x"))
	mustAddTab(t, &m)
	other := m.sess

	m.switchTab(0)
	m, _ = m.Command("/model fake/m2")
	m, _ = m.Command("/effort low")
	m.switchTab(1)
	m, _ = m.Command("/mode auto")
	m.toggleMode() // Shift+Tab path: back to manual
	m.toggleMode() // and auto again

	for name, s := range map[string]*agent.Session{"tab 0": sess, "tab 1": other} {
		if s.ModelID() != "fake/m2" || s.Effort() != "low" || s.Mode() != agent.ModeAuto {
			t.Errorf("%s: model %q effort %q mode %q, want fake/m2 low auto", name, s.ModelID(), s.Effort(), s.Mode())
		}
	}

	mustAddTab(t, &m)
	if s := m.sess; s.ModelID() != "fake/m2" || s.Effort() != "low" || s.Mode() != agent.ModeAuto {
		t.Errorf("added tab: model %q effort %q mode %q, want fake/m2 low auto", s.ModelID(), s.Effort(), s.Mode())
	}
	if row1, _ := m.StatusRows(); !strings.Contains(row1, "fake/m2") || !strings.Contains(row1, "low") {
		t.Errorf("status row 1 of the added tab = %q", row1)
	}
}

func TestCloseTabSwitchesLeftNeighbour(t *testing.T) {
	for _, tc := range []struct {
		name      string
		close     int // index of the tab closed
		wantCur   int
		wantSess  int // original index of the tab current afterwards
		remaining []int
	}{
		{"middle", 1, 0, 0, []int{0, 2}},
		{"last", 2, 1, 1, []int{0, 1}},
		{"first", 0, 0, 1, []int{1, 2}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m, _ := newSessionModel(t, answerServer(t, "x"))
			mustAddTab(t, &m)
			mustAddTab(t, &m)
			var orig []*agent.Session
			for i := range m.tabs {
				orig = append(orig, m.tabAt(i).sess)
			}
			m.switchTab(tc.close)
			m, cmd := m.closeTab()
			if cmd != nil {
				t.Fatal("closing one of three tabs returned a command")
			}
			if len(m.tabs) != 2 || m.cur != tc.wantCur {
				t.Fatalf("tabs = %d, cur = %d, want 2 and %d", len(m.tabs), m.cur, tc.wantCur)
			}
			if m.sess != orig[tc.wantSess] {
				t.Errorf("current tab is not the original tab %d", tc.wantSess)
			}
			for i, want := range tc.remaining {
				if m.tabAt(i).sess != orig[want] {
					t.Errorf("tab %d is not the original tab %d", i, want)
				}
			}
		})
	}
}

func TestCloseTabSavesSession(t *testing.T) {
	m, sess := newSessionModel(t, answerServer(t, "tab zero answer"))
	m = tabTurn(t, m, "tab zero question")
	mustAddTab(t, &m)
	other := m.sess

	m.switchTab(0)
	m, cmd := m.closeTab()
	if cmd != nil {
		if _, ok := cmd().(tea.QuitMsg); ok {
			t.Fatal("closing one of two tabs quit the program")
		}
	}
	if len(m.tabs) != 1 || m.cur != 0 || m.sess != other {
		t.Fatalf("tabs = %d, cur = %d, want the other tab only", len(m.tabs), m.cur)
	}
	_, raw := readSavedFile(t, sess.StartDir())
	if !strings.Contains(raw, "tab zero question") || !strings.Contains(raw, "tab zero answer") {
		t.Errorf("saved file lacks the closed tab's conversation:\n%s", raw)
	}
	if strings.Contains(raw, "SECRET123") {
		t.Error("saved file holds the secret")
	}
	if v := plainView(m); strings.Contains(v, "tab zero") {
		t.Errorf("remaining tab shows the closed conversation:\n%s", v)
	}
}

func TestAddTabAtLimitNoChange(t *testing.T) {
	m, sess := newSessionModel(t, answerServer(t, "x"))
	for len(m.tabs) < maxTabs {
		mustAddTab(t, &m)
	}
	m.switchTab(3)
	before := m.sess
	err := m.addTab()
	if !errors.Is(err, errTabLimit) {
		t.Fatalf("addTab at the limit = %v, want errTabLimit", err)
	}
	if len(m.tabs) != maxTabs || m.cur != 3 || m.sess != before {
		t.Fatalf("tabs = %d, cur = %d after the refused addTab", len(m.tabs), m.cur)
	}

	// /new at the limit: one info line, nothing else changes, the queue stays.
	m.queue = []string{"queued draft"}
	m, cmd := m.Command("/new")
	if cmd != nil {
		t.Error("/new at the limit returned a command")
	}
	if len(m.tabs) != maxTabs || m.cur != 3 {
		t.Fatalf("tabs = %d, cur = %d after /new at the limit", len(m.tabs), m.cur)
	}
	if got := lastBlockText(m); got != tabLimitMsg {
		t.Errorf("last block = %q, want %q", got, tabLimitMsg)
	}
	if len(m.queue) != 1 || m.queue[0] != "queued draft" {
		t.Errorf("queue = %q, want it kept", m.queue)
	}
	if files := sessionFiles(t, sess.StartDir()); len(files) != 0 {
		t.Errorf("files written: %v", files)
	}
}

func TestAddTabWithoutFactory(t *testing.T) {
	m, _ := newSessionModelOpts(t, nil, "start", func(o *Options) { o.NewSession = nil })
	if err := m.addTab(); err == nil || err.Error() != "could not open a new tab" {
		t.Fatalf("addTab = %v, want could not open a new tab", err)
	}
	m, _ = m.Command("/new")
	if len(m.tabs) != 1 {
		t.Fatalf("tabs = %d, want 1", len(m.tabs))
	}
	if v := plainView(m); !strings.Contains(v, "could not open a new tab") {
		t.Errorf("view lacks the error line:\n%s", v)
	}
}

func TestNewOpensTabKeepsOldFile(t *testing.T) {
	m, sess := newSessionModel(t, answerServer(t, "an answer"))
	m = tabTurn(t, m, "old conversation")
	m.saveTab()
	files := sessionFiles(t, sess.StartDir())
	if len(files) != 1 {
		t.Fatalf("session files = %v, want one", files)
	}
	old := files[0]

	m, _ = m.Command("/new")
	if len(m.tabs) != 2 || m.cur != 1 || m.sess == sess {
		t.Fatalf("tabs = %d, cur = %d after /new", len(m.tabs), m.cur)
	}
	m = tabTurn(t, m, "new conversation")
	m.saveTab()
	if !fileExists(old) {
		t.Fatal("the old session file is gone after /new and a save of the new tab")
	}
	if files := sessionFiles(t, sess.StartDir()); len(files) != 2 {
		t.Errorf("session files = %v, want the old one and the new one", files)
	}
	b, err := os.ReadFile(old)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), "old conversation") || strings.Contains(string(b), "new conversation") {
		t.Errorf("old file content changed:\n%s", b)
	}
}

func TestBackgroundTurnAppendsToItsTab(t *testing.T) {
	srv, release := heldServer(t)
	m, sess, clock := newClockModel(t, srv)
	m, _ = m.Command("background work")
	waitBusy(t, sess)
	if !m.turnActive {
		t.Fatal("no turn running in tab 0")
	}

	mustAddTab(t, &m)
	if v := belowTabBar(plainView(m)); strings.Contains(v, "Thinking…") || strings.Contains(v, "background work") {
		t.Fatalf("tab 1 shows tab 0's turn:\n%s", v)
	}
	clock.Advance(3e9)
	release()
	waitSession(t, sess)

	// Pump tab 0's events the way the program does (EventMsg carrying the
	// session) until its turn is done.
	for i := 0; ; i++ {
		if i > 100 {
			t.Fatal("no turn-done event")
		}
		msg := waitEventOf(m.tabAt(0))()
		ev, ok := msg.(EventMsg)
		if !ok {
			t.Fatalf("pump returned %T", msg)
		}
		next, _ := m.Update(ev)
		m = next.(Model)
		if ev.Event.Kind == agent.EvTurnDone {
			break
		}
	}

	if m.cur != 1 {
		t.Fatalf("cur = %d, want 1", m.cur)
	}
	if len(m.blocks) != 0 || m.turnActive {
		t.Fatalf("tab 1 got %d blocks, turnActive %v", len(m.blocks), m.turnActive)
	}
	v := belowTabBar(plainView(m))
	for _, bad := range []string{"● ok", "Worked for", "Interrupted after", "Thinking…", "background work"} {
		if strings.Contains(v, bad) {
			t.Errorf("tab 1 view has %q:\n%s", bad, v)
		}
	}

	m.switchTab(0)
	v = plainView(m)
	for _, want := range []string{"> background work", "ok", "✻ Worked for 3s"} {
		if !strings.Contains(v, want) {
			t.Errorf("tab 0 view lacks %q:\n%s", want, v)
		}
	}
	if m.turnActive || strings.Contains(v, "Thinking…") {
		t.Errorf("tab 0 still shows a running turn:\n%s", v)
	}
}
