package tui

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"yipyip/internal/agent"
	"yipyip/internal/provider/providertest"
	"yipyip/internal/session"
)

// noAutosave turns the autosave into a no-op for tests that check the exit
// save path on its own.
func noAutosave(o *Options) {
	o.Autosave = func(*session.SessionData) error { return nil }
}

// saveRecorder is an Autosave/PruneSessions seam that records its calls,
// assigns ids like the Store and fails while fail is set.
type saveRecorder struct {
	mu     sync.Mutex
	saves  []session.SessionData
	prunes [][]string
	keeps  []int
	fail   error
	nextID int
}

func (r *saveRecorder) save(d *session.SessionData) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.fail != nil {
		return r.fail
	}
	if d.ID == "" {
		r.nextID++
		d.ID = fmt.Sprintf("%012x", r.nextID)
	}
	r.saves = append(r.saves, *d)
	return nil
}

func (r *saveRecorder) prune(keep int, protect ...string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.keeps = append(r.keeps, keep)
	r.prunes = append(r.prunes, append([]string{}, protect...))
	return nil
}

func (r *saveRecorder) setFail(err error) {
	r.mu.Lock()
	r.fail = err
	r.mu.Unlock()
}

func (r *saveRecorder) count() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.saves)
}

func (r *saveRecorder) last() session.SessionData {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.saves[len(r.saves)-1]
}

func (r *saveRecorder) pruneCalls() ([]int, [][]string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return slices.Clone(r.keeps), slices.Clone(r.prunes)
}

// saveClock is a settable, locked Options.Now.
type saveClock struct {
	mu sync.Mutex
	t  time.Time
}

func (c *saveClock) now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.t
}

func (c *saveClock) set(t time.Time) {
	c.mu.Lock()
	c.t = t
	c.mu.Unlock()
}

// recModel returns a session model whose autosaves and prunes go to rec.
func recModel(t *testing.T, srv *providertest.Server, rec *saveRecorder, clock *saveClock) (Model, *agent.Session) {
	t.Helper()
	return newSessionModelOpts(t, srv, "start", func(o *Options) {
		o.Autosave = rec.save
		o.PruneSessions = rec.prune
		o.SaveOnExit = rec.save
		if clock != nil {
			o.Now = clock.now
		}
	})
}

// runCmds runs cmd synchronously, batches included, and returns the
// non-nil messages. The model's context must be cancelled first so event
// pumps return at once.
func runCmds(cmd tea.Cmd) []tea.Msg {
	if cmd == nil {
		return nil
	}
	msg := cmd()
	if b, ok := msg.(tea.BatchMsg); ok {
		var out []tea.Msg
		for _, c := range b {
			out = append(out, runCmds(c)...)
		}
		return out
	}
	if msg == nil {
		return nil
	}
	return []tea.Msg{msg}
}

// sendEvent feeds one main-agent event of the current tab through Update,
// runs the returned commands and feeds their messages back.
func sendEvent(m Model, kind agent.EventKind) Model {
	next, cmd := m.Update(EventMsg{Event: agent.Event{Kind: kind, AgentID: agent.MainAgentID}})
	m = next.(Model)
	for _, msg := range runCmds(cmd) {
		next, _ = m.Update(msg)
		m = next.(Model)
	}
	return m
}

// countAutosaveErrors counts the autosave error lines of the current tab.
func countAutosaveErrors(m Model) int {
	n := 0
	for _, b := range m.blocks {
		if b.kind == blockError && strings.HasPrefix(b.text, "could not autosave session") {
			n++
		}
	}
	return n
}

func TestAutosaveAfterTurnEnd(t *testing.T) {
	rec := &saveRecorder{}
	m, sess := recModel(t, answerServer(t, "the answer"), rec, nil)
	m, _ = m.Command("first question")
	waitSession(t, sess)
	m = m.Drain()
	if n := rec.count(); n != 1 {
		t.Fatalf("autosaves after one turn = %d, want 1", n)
	}
	d := rec.last()
	if d.Title != "first question" || d.Turn.Interrupted {
		t.Errorf("saved title %q interrupted %v", d.Title, d.Turn.Interrupted)
	}
	conv := d.Conversation
	if len(conv) < 3 || conv[len(conv)-1].Role != "assistant" || conv[len(conv)-1].Content != "the answer" {
		t.Errorf("saved conversation does not end with the answer: %+v", conv)
	}

	// The second turn overwrites the same id.
	id := d.ID
	m, _ = m.Command("second question")
	waitSession(t, sess)
	_ = m.Drain()
	if n := rec.count(); n != 2 {
		t.Fatalf("autosaves after two turns = %d, want 2", n)
	}
	if got := rec.last().ID; got != id {
		t.Errorf("second autosave id %q, want %q", got, id)
	}
}

func TestAutosaveSpacing(t *testing.T) {
	rec := &saveRecorder{}
	t0 := time.Date(2026, 10, 28, 12, 0, 0, 0, time.UTC)
	clock := &saveClock{t: t0}
	m, sess := recModel(t, answerServer(t, "hi"), rec, clock)
	m, _ = m.Command("question")
	waitSession(t, sess)
	m = m.Drain() // its checkpoint is too early, the turn end saves
	if n := rec.count(); n != 1 {
		t.Fatalf("autosaves after the turn = %d, want 1", n)
	}
	m.cancel() // event pumps return at once

	for _, step := range []struct {
		at   time.Duration
		kind agent.EventKind
		want int
	}{
		{10 * time.Second, agent.EvCheckpoint, 1},
		{59 * time.Second, agent.EvCheckpoint, 1},
		{61 * time.Second, agent.EvCheckpoint, 2},
		{62 * time.Second, agent.EvCheckpoint, 2},
		{63 * time.Second, agent.EvTurnDone, 3}, // a turn end always saves
		{64 * time.Second, agent.EvTurnDone, 4},
	} {
		clock.set(t0.Add(step.at))
		m = sendEvent(m, step.kind)
		if n := rec.count(); n != step.want {
			t.Fatalf("at +%v (kind %d): %d autosaves, want %d", step.at, step.kind, n, step.want)
		}
	}

	// Subagent events never autosave.
	clock.set(t0.Add(10 * time.Minute))
	next, cmd := m.Update(EventMsg{Event: agent.Event{Kind: agent.EvTurnDone, AgentID: "a1"}})
	m = next.(Model)
	runCmds(cmd)
	next, cmd = m.Update(EventMsg{Event: agent.Event{Kind: agent.EvCheckpoint, AgentID: "a1"}})
	runCmds(cmd)
	_ = next
	if n := rec.count(); n != 4 {
		t.Errorf("subagent events autosaved: %d saves, want 4", n)
	}
}

func TestAutosaveNotOnUIGoroutine(t *testing.T) {
	rec := &saveRecorder{}
	var (
		mu      sync.Mutex
		block   bool
		entered = make(chan struct{}, 4)
		release = make(chan struct{})
	)
	m, sess := newSessionModelOpts(t, answerServer(t, "hi"), "start", func(o *Options) {
		o.Autosave = func(d *session.SessionData) error {
			mu.Lock()
			b := block
			mu.Unlock()
			if b {
				entered <- struct{}{}
				<-release
			}
			return rec.save(d)
		}
		o.PruneSessions = rec.prune
	})
	m, _ = m.Command("question")
	waitSession(t, sess)
	m = m.Drain()
	if rec.count() != 1 {
		t.Fatalf("setup autosaves = %d, want 1", rec.count())
	}
	m.cancel()
	mu.Lock()
	block = true
	mu.Unlock()

	// update runs Update off the test goroutine so a blocking Update fails
	// the test instead of hanging it.
	update := func(m Model, msg tea.Msg) (Model, tea.Cmd) {
		t.Helper()
		type res struct {
			m   Model
			cmd tea.Cmd
		}
		ch := make(chan res, 1)
		go func() {
			next, cmd := m.Update(msg)
			ch <- res{next.(Model), cmd}
		}()
		select {
		case r := <-ch:
			return r.m, r.cmd
		case <-time.After(2 * time.Second):
			t.Fatal("Update blocked")
		}
		return m, nil
	}

	m, cmd := update(m, EventMsg{Event: agent.Event{Kind: agent.EvTurnDone, AgentID: agent.MainAgentID}})
	if rec.count() != 1 {
		t.Fatal("Update wrote the session itself")
	}
	msgs := make(chan []tea.Msg, 1)
	go func() { msgs <- runCmds(cmd) }()
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("the autosave command never reached the writer")
	}

	// The UI stays responsive while the write is blocked.
	for _, r := range "abc" {
		m, _ = update(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}
	if got := m.input.Value(); got != "abc" {
		t.Errorf("input = %q while the autosave is blocked, want abc", got)
	}
	_ = plainView(m)
	// A turn end meanwhile is queued behind the running write.
	m, cmd2 := update(m, EventMsg{Event: agent.Event{Kind: agent.EvTurnDone, AgentID: agent.MainAgentID}})
	go runCmds(cmd2)

	close(release)
	select {
	case <-msgs:
	case <-time.After(5 * time.Second):
		t.Fatal("the autosave did not finish after the release")
	}
	if n := rec.count(); n != 3 {
		t.Errorf("autosaves = %d, want 3 (setup, blocked, queued)", n)
	}
	if rec.last().ID != "000000000001" {
		t.Errorf("queued autosave id %q, want the first id", rec.last().ID)
	}
}

func TestAutosaveErrorOnce(t *testing.T) {
	rec := &saveRecorder{}
	rec.setFail(errors.New("disk full SECRET123"))
	m, sess := recModel(t, answerServer(t, "hi"), rec, nil)
	m, _ = m.Command("question")
	waitSession(t, sess)
	m = m.Drain() // failing save 1
	m.cancel()
	m = sendEvent(m, agent.EvTurnDone) // failing save 2
	m = sendEvent(m, agent.EvTurnDone) // failing save 3
	if n := countAutosaveErrors(m); n != 1 {
		t.Fatalf("%d autosave error lines after 3 failures, want 1", n)
	}
	v := plainView(m)
	if !strings.Contains(v, "could not autosave session: disk full") {
		t.Errorf("view lacks the autosave error line:\n%s", v)
	}
	if strings.Contains(v, "SECRET123") {
		t.Errorf("view shows the secret:\n%s", v)
	}

	rec.setFail(nil)
	m = sendEvent(m, agent.EvTurnDone)
	if rec.count() != 1 {
		t.Fatalf("successful autosaves = %d, want 1", rec.count())
	}
	rec.setFail(errors.New("disk full again"))
	m = sendEvent(m, agent.EvTurnDone)
	m = sendEvent(m, agent.EvTurnDone)
	if n := countAutosaveErrors(m); n != 2 {
		t.Fatalf("%d autosave error lines after a success and new failures, want 2", n)
	}
}

func TestAutosaveErrorInBackgroundTab(t *testing.T) {
	rec := &saveRecorder{}
	m, sess := recModel(t, answerServer(t, "hi"), rec, nil)
	m, _ = m.Command("question")
	waitSession(t, sess)
	m = m.Drain()
	m.cancel()
	mustAddTab(t, &m)
	rec.setFail(errors.New("disk full"))
	next, cmd := m.Update(EventMsg{Event: agent.Event{Kind: agent.EvTurnDone, AgentID: agent.MainAgentID}, sess: sess})
	m = next.(Model)
	for _, msg := range runCmds(cmd) {
		next, _ = m.Update(msg)
		m = next.(Model)
	}
	if n := countAutosaveErrors(m); n != 0 {
		t.Errorf("the error landed in the current tab")
	}
	m.switchTab(0)
	if n := countAutosaveErrors(m); n != 1 {
		t.Errorf("first tab has %d autosave error lines, want 1", n)
	}
}

func TestAutosaveInterruptedMidTurn(t *testing.T) {
	rec := &saveRecorder{}
	t0 := time.Date(2026, 10, 28, 12, 0, 0, 0, time.UTC)
	clock := &saveClock{t: t0}
	srv := providertest.NewFunc(func(i int, _ providertest.Request) providertest.Response {
		if i == 0 {
			return providertest.Response{ToolCalls: []providertest.ToolCall{
				{ID: "c1", Name: "read_file", Arguments: `{"path":"note.txt"}`},
			}}
		}
		return providertest.Response{Text: "late", Delay: 30 * time.Second}
	})
	t.Cleanup(srv.Close)
	m, sess := recModel(t, srv, rec, clock)
	if err := os.WriteFile(filepath.Join(sess.StartDir(), "note.txt"), []byte("note\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	m, _ = m.Command("read the note")
	waitRequests(t, srv, 2) // the first round is complete and checkpointed
	clock.set(t0.Add(2 * time.Minute))
	m = m.Drain()
	if rec.count() != 1 {
		t.Fatalf("mid-turn autosaves = %d, want 1", rec.count())
	}
	d := rec.last()
	if !d.Turn.Interrupted {
		t.Error("a mid-turn snapshot is not stored as interrupted")
	}
	answered := map[string]bool{}
	for i := len(d.Conversation) - 1; i >= 0; i-- {
		c := d.Conversation[i]
		if c.Role == "tool" {
			answered[c.ToolCallID] = true
		}
		for _, tc := range c.ToolCalls {
			if !answered[tc.ID] {
				t.Errorf("dangling tool call %q in %+v", tc.ID, d.Conversation)
			}
		}
	}
	if last := d.Conversation[len(d.Conversation)-1]; last.Role != "tool" || last.ToolCallID != "c1" {
		t.Errorf("snapshot does not end at the checkpoint: %+v", d.Conversation)
	}
	sess.Cancel()
	waitSession(t, sess)
	_ = m
}

func TestAutosaveEmptyConversationSkipped(t *testing.T) {
	rec := &saveRecorder{}
	t0 := time.Date(2026, 10, 28, 12, 0, 0, 0, time.UTC)
	clock := &saveClock{t: t0}
	m, _ := recModel(t, answerServer(t, "hi"), rec, clock)
	m.cancel()
	m = sendEvent(m, agent.EvTurnDone)
	clock.set(t0.Add(time.Hour))
	m = sendEvent(m, agent.EvCheckpoint)
	if n := rec.count(); n != 0 {
		t.Errorf("empty conversation autosaved %d times", n)
	}
	// No session at all.
	nm := newBlockModel().(Model)
	nm.opts.Autosave = rec.save
	nm.cancel()
	_ = sendEvent(nm, agent.EvTurnDone)
	if n := rec.count(); n != 0 {
		t.Errorf("session-less tab autosaved %d times", n)
	}
	if keeps, _ := rec.pruneCalls(); len(keeps) != 0 {
		t.Errorf("prune called %d times without a save", len(keeps))
	}
}

func TestAutosaveExitSingleFile(t *testing.T) {
	entered, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	m, sess := newSessionModelOpts(t, answerServer(t, "hi"), "start", func(o *Options) {
		startDir := o.StartDir
		o.Autosave = func(d *session.SessionData) error {
			first := false
			once.Do(func() { first = true })
			if first {
				close(entered)
				<-release
			}
			st, err := session.NewStore(startDir)
			if err != nil {
				return err
			}
			return st.Save(d)
		}
	})
	m, _ = m.Command("question")
	waitSession(t, sess)
	m.cancel()
	var cmds []tea.Cmd
	for _, ev := range sess.PollEvents() {
		next, cmd := m.Update(EventMsg{Event: ev, sess: sess})
		m = next.(Model)
		cmds = append(cmds, cmd)
	}
	writer := make(chan struct{})
	go func() {
		for _, c := range cmds {
			runCmds(c)
		}
		close(writer)
	}()
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("the first autosave never started")
	}

	exited := make(chan struct{})
	go func() {
		m.Command("/exit")
		close(exited)
	}()
	time.Sleep(100 * time.Millisecond) // the exit save waits for the autosave
	close(release)
	for _, ch := range []chan struct{}{exited, writer} {
		select {
		case <-ch:
		case <-time.After(10 * time.Second):
			t.Fatal("exit or autosave did not finish")
		}
	}
	files := sessionFiles(t, sess.StartDir())
	if len(files) != 1 {
		t.Fatalf("session files = %v, want exactly one", files)
	}
	_, raw := readSavedFile(t, sess.StartDir())
	if strings.Contains(raw, "SECRET123") {
		t.Error("session file holds the secret")
	}
}

func TestAutosavePrunesProtectingTabs(t *testing.T) {
	rec := &saveRecorder{}
	m, sess := recModel(t, answerServer(t, "hi"), rec, nil)
	m = tabTurn(t, m, "first tab")
	mustAddTab(t, &m)
	m = tabTurn(t, m, "second tab")
	if rec.count() != 2 {
		t.Fatalf("autosaves = %d, want 2", rec.count())
	}
	id1, id2 := rec.saves[0].ID, rec.saves[1].ID
	keeps, prunes := rec.pruneCalls()
	if len(prunes) != 2 {
		t.Fatalf("prune calls = %d, want 2", len(prunes))
	}
	for _, k := range keeps {
		if k != 25 {
			t.Errorf("prune keep = %d, want 25", k)
		}
	}
	if !slices.Contains(prunes[0], id1) {
		t.Errorf("first prune protects %v, want %s", prunes[0], id1)
	}
	if !slices.Contains(prunes[1], id1) || !slices.Contains(prunes[1], id2) {
		t.Errorf("second prune protects %v, want %s and %s", prunes[1], id1, id2)
	}

	// A failed save never prunes.
	m.cancel()
	rec.setFail(errors.New("disk full"))
	m = sendEvent(m, agent.EvTurnDone)
	if keeps, _ := rec.pruneCalls(); len(keeps) != 2 {
		t.Errorf("prune called after a failed save: %d calls", len(keeps))
	}

	// The exit save prunes too, protecting both tabs.
	rec.setFail(nil)
	m.Command("/exit")
	keeps, prunes = rec.pruneCalls()
	if len(keeps) != 4 {
		t.Fatalf("prune calls after exit = %d, want 4 (one per tab)", len(keeps))
	}
	for _, p := range prunes[2:] {
		if !slices.Contains(p, id1) || !slices.Contains(p, id2) {
			t.Errorf("exit prune protects %v, want %s and %s", p, id1, id2)
		}
	}
	_ = sess
}
