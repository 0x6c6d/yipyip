package tui

import (
	"fmt"
	"io/fs"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/muesli/termenv"

	"yipyip/internal/agent"
	"yipyip/internal/provider/providertest"
)

func suggestSet(m Model, v string) Model {
	m.input.SetValue(v)
	m.input.CursorEnd()
	return m
}

func suggestServer(t *testing.T) *providertest.Server {
	t.Helper()
	srv := providertest.New()
	t.Cleanup(srv.Close)
	return srv
}

func TestSuggestCommand(t *testing.T) {
	m := suggestSet(New(Options{StartDir: "/w"}), "/eff")
	ghost, accept := m.suggestion()
	if ghost != "ort" || accept != "/effort" {
		t.Fatalf("ghost, accept = %q, %q", ghost, accept)
	}
	if g, _ := suggestSet(m, "/effort").suggestion(); g != "" {
		t.Fatalf("exact match has ghost %q", g)
	}
}

func TestSuggestEffortHint(t *testing.T) {
	m := New(Options{StartDir: "/w"})
	for in, want := range map[string]string{
		"/effort ": "[low|medium|high]",
		"/mode ":   "[auto|manual]",
		"/model ":  "[provider/model]",
	} {
		ghost, accept := suggestSet(m, in).suggestion()
		if ghost != want || accept != "" {
			t.Errorf("%q: ghost, accept = %q, %q; want %q, \"\"", in, ghost, accept, want)
		}
	}
}

func TestSuggestEffortPrefix(t *testing.T) {
	m := suggestSet(New(Options{StartDir: "/w"}), "/effort m")
	ghost, accept := m.suggestion()
	if ghost != "edium" || accept != "/effort medium" {
		t.Fatalf("ghost, accept = %q, %q", ghost, accept)
	}
	for _, in := range []string{"/effort medium", "/effort x", "/mode a"} {
		g, _ := suggestSet(m, in).suggestion()
		if in == "/mode a" {
			if g != "uto" {
				t.Errorf("%q: ghost %q", in, g)
			}
		} else if g != "" {
			t.Errorf("%q: ghost %q, want none", in, g)
		}
	}
}

func TestSuggestModelIDs(t *testing.T) {
	srv := suggestServer(t)
	m, sess := newSessionModel(t, srv)
	ids := sess.Models()
	if len(ids) == 0 {
		t.Fatal("no model ids configured")
	}
	first := ids[0]
	m = suggestSet(m, "/model "+first[:1])
	ghost, accept := m.suggestion()
	if ghost != first[1:] || accept != "/model "+first {
		t.Fatalf("ghost, accept = %q, %q; want %q", ghost, accept, first[1:])
	}
	if g, _ := suggestSet(New(Options{StartDir: "/w"}), "/model f").suggestion(); g != "" {
		t.Fatalf("model ghost without session: %q", g)
	}
}

func TestSuggestTabAccepts(t *testing.T) {
	var tm tea.Model = suggestSet(New(Options{StartDir: "/w"}), "/effort m")
	tm, _ = tm.Update(tea.KeyMsg{Type: tea.KeyTab})
	if got := tm.(Model).input.Value(); got != "/effort medium" {
		t.Fatalf("Tab: input = %q", got)
	}
	if g, _ := tm.(Model).suggestion(); g != "" {
		t.Fatalf("ghost after accept: %q", g)
	}

	tm = suggestSet(New(Options{StartDir: "/w"}), "/eff")
	tm, _ = tm.Update(tea.KeyMsg{Type: tea.KeyRight})
	if got := tm.(Model).input.Value(); got != "/effort" {
		t.Fatalf("Right: input = %q", got)
	}
}

func TestSuggestHintNotAccepted(t *testing.T) {
	for _, k := range []tea.KeyType{tea.KeyTab, tea.KeyRight} {
		var tm tea.Model = suggestSet(New(Options{StartDir: "/w"}), "/effort ")
		tm, _ = tm.Update(tea.KeyMsg{Type: k})
		if got := tm.(Model).input.Value(); got != "/effort " {
			t.Fatalf("key %v: input = %q", k, got)
		}
	}
}

func TestSuggestNoGhost(t *testing.T) {
	base := New(Options{StartDir: "/w"})
	if g, a := suggestSet(base, "/eff\nx").suggestion(); g != "" || a != "" {
		t.Errorf("multi-line: %q %q", g, a)
	}
	m := suggestSet(base, "/eff")
	m.input.SetCursor(2)
	if g, a := m.suggestion(); g != "" || a != "" {
		t.Errorf("cursor not at end: %q %q", g, a)
	}
	if g, a := suggestSet(base, "eff").suggestion(); g != "" || a != "" {
		t.Errorf("no slash: %q %q", g, a)
	}
	pm := suggestSet(base, "/eff")
	pm.picker.open = true
	if g, _ := pm.suggestion(); g != "" {
		t.Errorf("picker open: %q", g)
	}
}

func TestSuggestColor(t *testing.T) {
	withColorProfile(t, termenv.ANSI256)
	m := suggestSet(New(Options{StartDir: "/w"}), "/eff")
	m.resize(80, 24)
	want := lipgloss.NewStyle().Foreground(lipgloss.Color("244")).Render("ort")
	if v := m.View(); !strings.Contains(v, want) {
		t.Fatalf("ghost %q missing:\n%q", want, v)
	}
	if v := m.input.Value(); v != "/eff" {
		t.Fatalf("ghost leaked into the value: %q", v)
	}
}

func TestSuggestNeverSent(t *testing.T) {
	srv := suggestServer(t)
	m, sess := newSessionModel(t, srv)
	m = suggestSet(m, "hello")
	if g, _ := m.suggestion(); g != "" {
		t.Fatalf("ghost on plain text: %q", g)
	}
	m = suggestSet(m, "/eff")
	if g, _ := m.suggestion(); g != "ort" {
		t.Fatalf("ghost = %q", g)
	}
	histLen := len(sess.History())
	tm, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if v := ansi.Strip(tm.View()); !strings.Contains(v, "unknown command: /eff") || strings.Contains(v, "unknown command: /effort") {
		t.Fatalf("Enter did not run /eff:\n%s", v)
	}
	if h := sess.History(); len(h) != histLen {
		t.Fatalf("history changed: %d -> %d", histLen, len(h))
	}
	if n := len(srv.Requests()); n != 0 {
		t.Fatalf("provider got %d requests", n)
	}
	for _, msg := range sess.History()[1:] {
		if strings.Contains(msg.Content, "ort") {
			t.Fatalf("history contains the ghost: %q", msg.Content)
		}
	}
}

// histSend submits text through the input box and waits for any turn.
func histSend(t *testing.T, m Model, sess *agent.Session, text string) Model {
	t.Helper()
	m.input.SetValue(text)
	tm, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	waitSession(t, sess)
	return tm.(Model).Drain()
}

// histKey sends one key and returns the input value afterwards.
func histKey(m Model, k tea.KeyType) (Model, string) {
	tm, _ := m.Update(tea.KeyMsg{Type: k})
	nm := tm.(Model)
	return nm, nm.input.Value()
}

// histModel returns a session model after sending one, /mode auto, three.
func histModel(t *testing.T) (Model, *agent.Session, *providertest.Server) {
	t.Helper()
	srv := suggestServer(t)
	m, sess := newSessionModel(t, srv)
	for _, s := range []string{"one", "/mode auto", "three"} {
		m = histSend(t, m, sess, s)
	}
	return m, sess, srv
}

func histSteps(t *testing.T, m Model, steps []tea.KeyType, want []string) Model {
	t.Helper()
	var got string
	for i, k := range steps {
		m, got = histKey(m, k)
		if got != want[i] {
			t.Fatalf("step %d (%v): input = %q, want %q", i, k, got, want[i])
		}
	}
	return m
}

func TestHistoryUpDownOrder(t *testing.T) {
	m, _, _ := histModel(t)
	up, down := tea.KeyUp, tea.KeyDown
	histSteps(t, m,
		[]tea.KeyType{up, up, up, up, down, down},
		[]string{"three", "/mode auto", "one", "one", "/mode auto", "three"})
}

func TestHistoryDraftRestored(t *testing.T) {
	m, _, _ := histModel(t)
	m = typeText(m, "draft").(Model)
	m = histSteps(t, m,
		[]tea.KeyType{tea.KeyUp, tea.KeyUp, tea.KeyDown, tea.KeyDown},
		[]string{"three", "/mode auto", "three", "draft"})
	if m.hist.navigating() {
		t.Fatal("still navigating after the draft was restored")
	}
	// Not navigating any more: Down leaves the draft alone.
	if _, got := histKey(m, tea.KeyDown); got != "draft" {
		t.Fatalf("Down after the draft: input = %q", got)
	}
}

func TestHistoryDuplicateCollapse(t *testing.T) {
	srv := suggestServer(t)
	m, sess := newSessionModel(t, srv)
	for _, s := range []string{"earlier", "same", "same"} {
		m = histSend(t, m, sess, s)
	}
	if want := []string{"earlier", "same"}; !reflect.DeepEqual(m.hist.entries, want) {
		t.Fatalf("entries = %q, want %q", m.hist.entries, want)
	}
	histSteps(t, m, []tea.KeyType{tea.KeyUp, tea.KeyUp}, []string{"same", "earlier"})
}

func TestHistoryCap500(t *testing.T) {
	m := New(Options{StartDir: "/w"})
	for i := 1; i <= 510; i++ {
		m.hist.push(fmt.Sprintf("entry %d", i))
	}
	if n := len(m.hist.entries); n != 500 {
		t.Fatalf("len = %d, want 500", n)
	}
	if first, last := m.hist.entries[0], m.hist.entries[499]; first != "entry 11" || last != "entry 510" {
		t.Fatalf("oldest, newest = %q, %q", first, last)
	}
}

func TestHistoryMultilineMovesCursor(t *testing.T) {
	m, _, _ := histModel(t)
	m.input.SetValue("a\nb")
	if l := m.input.Line(); l != 1 {
		t.Fatalf("cursor line = %d, want 1", l)
	}
	m, got := histKey(m, tea.KeyUp)
	if got != "a\nb" || m.input.Line() != 0 {
		t.Fatalf("first Up: input = %q, line %d; want the cursor moved to line 0", got, m.input.Line())
	}
	if m.hist.navigating() {
		t.Fatal("first Up started history navigation")
	}
	if _, got = histKey(m, tea.KeyUp); got != "three" {
		t.Fatalf("second Up: input = %q, want %q", got, "three")
	}
}

func TestHistorySurvivesNew(t *testing.T) {
	// v3: /new opens a new tab with its own (empty) history; the old tab
	// keeps its history.
	for _, cmd := range []string{"/new"} {
		t.Run(cmd, func(t *testing.T) {
			m, sess, _ := histModel(t)
			m = histSend(t, m, sess, cmd)
			if _, got := histKey(m, tea.KeyUp); got != "" {
				t.Fatalf("new tab: Up = %q, want nothing", got)
			}
			m.switchTab(0)
			histSteps(t, m,
				[]tea.KeyType{tea.KeyUp, tea.KeyUp, tea.KeyUp},
				[]string{cmd, "three", "/mode auto"})
		})
	}
}

func TestHistoryRecallEditResend(t *testing.T) {
	m, sess, srv := histModel(t)
	m, got := histKey(m, tea.KeyUp)
	if got != "three" {
		t.Fatalf("Up: input = %q", got)
	}
	m = typeText(m, "!").(Model)
	if v := m.input.Value(); v != "three!" {
		t.Fatalf("edited input = %q", v)
	}
	before := len(srv.Requests())
	tm, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	waitSession(t, sess)
	m = tm.(Model).Drain()
	reqs := srv.Requests()
	if len(reqs) != before+1 {
		t.Fatalf("requests %d -> %d, want one more", before, len(reqs))
	}
	msgs := reqs[len(reqs)-1].Messages
	if last := msgs[len(msgs)-1]; last.Role != "user" || last.Content != "three!" {
		t.Fatalf("last message = %+v, want user %q", last, "three!")
	}
	if n := len(m.hist.entries); m.hist.entries[n-1] != "three!" {
		t.Fatalf("newest entry = %q", m.hist.entries[n-1])
	}
}

// listFiles returns every path below root (directories included), sorted.
func listFiles(t *testing.T, root string) []string {
	t.Helper()
	var out []string
	err := filepath.WalkDir(root, func(p string, _ fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		out = append(out, p)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func TestHistoryNoFiles(t *testing.T) {
	srv := suggestServer(t)
	m, sess := newSessionModelOpts(t, srv, "start", noAutosave)
	root := filepath.Dir(sess.StartDir()) // holds the temp HOME and the start dir
	before := listFiles(t, root)
	for _, s := range []string{"one", "/mode auto", "/effort high", "three", "/new", "four"} {
		m = histSend(t, m, sess, s)
	}
	for _, k := range []tea.KeyType{tea.KeyUp, tea.KeyUp, tea.KeyUp, tea.KeyDown, tea.KeyUp, tea.KeyDown, tea.KeyDown, tea.KeyDown} {
		m, _ = histKey(m, k)
	}
	m = histSend(t, m, sess, "five")
	if len(m.hist.entries) == 0 {
		t.Fatal("no history recorded")
	}
	if after := listFiles(t, root); !reflect.DeepEqual(before, after) {
		t.Fatalf("files changed:\nbefore %q\nafter  %q", before, after)
	}
}

// pasteKey returns the key message of a bracketed paste of text.
func pasteKey(text string) tea.KeyMsg {
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(text), Paste: true}
}

func pasteInto(m Model, text string) Model {
	tm, _ := m.Update(pasteKey(text))
	return tm.(Model)
}

// pasteSend presses Enter, waits for the turn and returns the content of the
// last user message the provider received.
func pasteSend(t *testing.T, m Model, sess *agent.Session, srv *providertest.Server) (Model, string) {
	t.Helper()
	before := len(srv.Requests())
	tm, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	waitSession(t, sess)
	m = tm.(Model).Drain()
	reqs := srv.Requests()
	if len(reqs) != before+1 {
		t.Fatalf("requests %d -> %d, want one more", before, len(reqs))
	}
	msgs := reqs[len(reqs)-1].Messages
	last := msgs[len(msgs)-1]
	if last.Role != "user" {
		t.Fatalf("last message = %+v, want a user message", last)
	}
	return m, last.Content
}

func TestPasteMultilinePlaceholder(t *testing.T) {
	m := pasteInto(New(Options{StartDir: "/w"}), "alpha\nbeta\ngamma")
	if v := m.input.Value(); v != "[Pasted text #1 +3 lines]" {
		t.Fatalf("input = %q", v)
	}
	if v := ansi.Strip(m.View()); strings.Contains(v, "beta") {
		t.Fatalf("view shows the pasted text:\n%s", v)
	}
}

func TestPasteLongSingleLine(t *testing.T) {
	m := pasteInto(New(Options{StartDir: "/w"}), strings.Repeat("x", 100))
	if v := m.input.Value(); v != "[Pasted text #1 · 100 chars]" {
		t.Fatalf("input = %q", v)
	}
	if v := ansi.Strip(m.View()); strings.Contains(v, strings.Repeat("x", 20)) {
		t.Fatalf("view shows the pasted text:\n%s", v)
	}
}

func TestPasteShortVerbatim(t *testing.T) {
	m := pasteInto(New(Options{StartDir: "/w"}), "0123456789")
	if v := m.input.Value(); v != "0123456789" {
		t.Fatalf("input = %q", v)
	}
	if n := len(m.pastes.texts); n != 0 {
		t.Fatalf("store holds %d entries", n)
	}
}

func TestPasteExpandedOnSend(t *testing.T) {
	srv := suggestServer(t)
	m, sess := newSessionModel(t, srv)
	m = typeText(m, "see ").(Model)
	m = pasteInto(m, "alpha\nbeta\ngamma")
	if v := m.input.Value(); v != "see [Pasted text #1 +3 lines]" {
		t.Fatalf("input = %q", v)
	}
	m, content := pasteSend(t, m, sess, srv)
	if !strings.Contains(content, "see alpha\nbeta\ngamma") {
		t.Fatalf("request content = %q", content)
	}
	v := belowTabBar(ansi.Strip(m.View()))
	if !strings.Contains(v, "> see [Pasted text #1 +3 lines]") || strings.Contains(v, "gamma") {
		t.Fatalf("view:\n%s", v)
	}
	m, got := histKey(m, tea.KeyUp)
	if got != "see [Pasted text #1 +3 lines]" {
		t.Fatalf("Up: input = %q", got)
	}
	// A recalled message is expanded again.
	if _, content = pasteSend(t, m, sess, srv); !strings.Contains(content, "see alpha\nbeta\ngamma") {
		t.Fatalf("recalled request content = %q", content)
	}
}

func TestPasteEditedSentLiterally(t *testing.T) {
	srv := suggestServer(t)
	m, sess := newSessionModel(t, srv)
	m = pasteInto(m, "alpha\nbeta\ngamma")
	m, _ = histKey(m, tea.KeyBackspace)
	m = typeText(m, "X").(Model)
	if v := m.input.Value(); v != "[Pasted text #1 +3 linesX" {
		t.Fatalf("input = %q", v)
	}
	_, content := pasteSend(t, m, sess, srv)
	if !strings.Contains(content, "[Pasted text #1 +3 linesX") || strings.Contains(content, "beta") {
		t.Fatalf("request content = %q", content)
	}
}

func TestPasteCounter(t *testing.T) {
	srv := suggestServer(t)
	m, sess := newSessionModel(t, srv)
	m = pasteInto(m, "a\nb")
	if v := m.input.Value(); v != "[Pasted text #1 +2 lines]" {
		t.Fatalf("first paste: input = %q", v)
	}
	m.input.Reset()
	m = pasteInto(m, "c\nd")
	if v := m.input.Value(); v != "[Pasted text #2 +2 lines]" {
		t.Fatalf("second paste: input = %q", v)
	}
	// v3: /new opens a new tab with its own paste store; the old tab's
	// counter goes on.
	m = histSend(t, m, sess, "/new")
	m = pasteInto(m, "e\nf")
	if v := m.input.Value(); v != "[Pasted text #1 +2 lines]" {
		t.Fatalf("paste in the new tab: input = %q", v)
	}
	m.input.Reset()
	m.switchTab(0)
	m = pasteInto(m, "g\nh")
	if v := m.input.Value(); v != "[Pasted text #3 +2 lines]" {
		t.Fatalf("paste after /new in the old tab: input = %q", v)
	}
}

func TestPasteCRLF(t *testing.T) {
	srv := suggestServer(t)
	m, sess := newSessionModel(t, srv)
	m = pasteInto(m, "a\r\nb")
	if v := m.input.Value(); v != "[Pasted text #1 +2 lines]" {
		t.Fatalf("input = %q", v)
	}
	if _, content := pasteSend(t, m, sess, srv); !strings.Contains(content, "a\nb") || strings.Contains(content, "\r") {
		t.Fatalf("request content = %q", content)
	}
}
