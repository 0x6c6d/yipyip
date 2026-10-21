package tui

import (
	"context"
	"errors"
	"slices"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"yipyip/internal/agent"
)

// maxTabs is the most tabs (sessions) one window holds.
const maxTabs = 8

// tabLimitMsg is the line printed to the current tab when a new tab would
// exceed maxTabs.
const tabLimitMsg = "tab limit reached"

// errTabLimit is returned by addTab at maxTabs tabs.
var errTabLimit = errors.New(tabLimitMsg)

// errNewTab is returned by addTab when no agent session can be built for the
// new tab.
var errNewTab = errors.New("could not open a new tab")

// tab is the per-tab state: one agent session with its conversation, input
// history, pastes, queue, saved-session state and turn state. The fields
// keep the names they had on Model. The Model embeds the current tab, so
// m.blocks, m.sess, ... are always the current tab's.
type tab struct {
	sess *agent.Session // the agent session of this tab (nil = none)

	// pump is cancelled when the tab closes; it ends the tab's event command.
	pump     context.Context
	stopPump context.CancelFunc

	blocks    []block
	streaming bool                     // the last block is an assistant block still receiving deltas
	pending   []*agent.ApprovalRequest // approval requests waiting for y/n/a, FIFO

	questions []*agent.QuestionRequest // question-tool requests waiting for an answer, FIFO (copy-on-write)
	qs        questionState            // row cursor, marks and free text of questions[0]

	turnActive  bool      // a turn is running: the Thinking line is shown and ticks go on
	turnStart   time.Time // when the running turn was submitted
	turnOut0    int       // session output tokens at submit
	openCalls   int       // main-agent tool calls without a result yet
	interrupted bool      // the user cancelled the running turn (Esc or first Ctrl+C)
	retryStatus string    // "retrying (n/max) in <wait>" while a provider retry wait is pending, else ""

	subs map[string]*subState // live subagent blocks by subagent id

	turnStarts []int // block index where each undoable submitted turn starts (copy-on-write)

	hist   *inputHistory // submitted inputs for Up/Down recall, shared by all copies
	pastes *pasteStore   // paste placeholders and their full texts, shared by all copies

	queue []string // messages typed while busy, sent at the turn end (copy-on-write)

	rmPending string     // session id awaiting the y/n reply of /sessions rm
	saved     *saveState // created time and id of the saved conversation, shared by all copies

	// Kept while the tab is not current.
	draft   string // the input text
	follow  bool   // the viewport was at the bottom
	yOffset int    // the viewport's scroll position
}

// newTab returns an empty tab around sess; its event pump stops with parent.
func newTab(parent context.Context, sess *agent.Session) tab {
	pump, stop := context.WithCancel(parent)
	return tab{
		sess:     sess,
		pump:     pump,
		stopPump: stop,
		subs:     map[string]*subState{},
		hist:     &inputHistory{},
		pastes:   &pasteStore{},
		saved:    &saveState{},
		follow:   true,
	}
}

// tabAt returns a copy of tab i (the live current tab for i == cur). It is
// for reading only.
func (m *Model) tabAt(i int) tab {
	if i == m.cur {
		return m.tab
	}
	return m.tabs[i]
}

// tabOf returns the index of the tab whose agent session is sess, or -1.
func (m *Model) tabOf(sess *agent.Session) int {
	for i := range m.tabs {
		if m.tabAt(i).sess == sess {
			return i
		}
	}
	return -1
}

// park stores the live current tab into a copy of the tab list (the slice
// is copy-on-write, so other Model copies never see the change). Outside a
// background update the input draft and the scroll position go with it.
func (m *Model) park() {
	t := m.tab
	if !m.background {
		t.draft = m.input.Value()
		t.follow = m.viewport.AtBottom()
		t.yOffset = m.viewport.YOffset
	}
	m.tabs = slices.Clone(m.tabs)
	m.tabs[m.cur] = t
	m.tab = t
}

// load makes tab i the live current tab and shows it: input draft,
// conversation and scroll position.
func (m *Model) load(i int) {
	m.cur, m.tab = i, m.tabs[i]
	m.sel = selection{}
	m.input.Reset()
	if m.draft != "" {
		m.setInput(m.draft)
	}
	m.refreshTo(m.follow)
	if !m.follow {
		m.viewport.SetYOffset(m.yOffset)
	}
}

// inTab runs fn with tab i as the live tab: agent events, saves and
// restores of a tab that is not current land in that tab, never in the
// current one. The viewport is left alone for a background tab.
func (m *Model) inTab(i int, fn func(*Model)) {
	if i == m.cur {
		fn(m)
		return
	}
	if i < 0 || i >= len(m.tabs) {
		return
	}
	home, bg := m.cur, m.background
	m.background = true
	m.park()
	m.cur, m.tab = i, m.tabs[i]
	fn(m)
	m.park()
	m.cur, m.tab = home, m.tabs[home]
	m.background = bg
}

// sessions returns the agent sessions of all tabs (nil entries skipped).
func (m *Model) sessions() []*agent.Session {
	var out []*agent.Session
	for i := range m.tabs {
		if s := m.tabAt(i).sess; s != nil {
			out = append(out, s)
		}
	}
	return out
}

// globals returns the model, effort and mode selection shared by all tabs.
func (m *Model) globals() (model, effort, mode string) {
	if sess := m.sess; sess != nil {
		return sess.ModelID(), sess.Effort(), string(sess.Mode())
	}
	return m.opts.ModelID, m.opts.Effort, m.opts.Mode
}

// setGlobals applies one model, effort and mode selection to the agent
// sessions of all tabs; new tabs inherit it (see addTab).
func (m *Model) setGlobals(model, effort, mode string) {
	m.opts.ModelID, m.opts.Effort, m.opts.Mode = model, effort, mode
	for _, s := range m.sessions() {
		s.RestoreModelEffortMode(model, effort, agent.Mode(mode))
	}
}

// newSession builds the agent session of a new tab through
// Options.NewSession. Without a factory a model that has no session gets a
// session-less tab, and one that has a session reports errNewTab.
func (m *Model) newSession() (*agent.Session, error) {
	if m.opts.NewSession == nil {
		if m.sess == nil {
			return nil, nil
		}
		return nil, errNewTab
	}
	sess, err := m.opts.NewSession()
	if err != nil || sess == nil {
		return nil, errNewTab
	}
	return sess, nil
}

// addTab appends a fresh session tab that inherits the global model, effort
// and mode and makes it current. At maxTabs nothing changes and errTabLimit
// is returned. The caller starts the new tab's event pump (m.waitEvent).
func (m *Model) addTab() error {
	if len(m.tabs) >= maxTabs {
		return errTabLimit
	}
	sess, err := m.newSession()
	if err != nil {
		return err
	}
	model, effort, mode := m.globals()
	if sess != nil {
		sess.RestoreModelEffortMode(model, effort, agent.Mode(mode))
	}
	m.park()
	m.tabs = append(m.tabs, newTab(m.ctx, sess))
	m.load(len(m.tabs) - 1)
	return nil
}

// switchTab makes tab i current; an index out of range changes nothing.
func (m *Model) switchTab(i int) {
	if i < 0 || i >= len(m.tabs) || i == m.cur {
		return
	}
	m.park()
	m.load(i)
}

// closeTab saves the current tab through the exit save path and removes it;
// the left neighbour becomes current, else the right one. Closing the last
// tab is the /exit path: save, then quit.
func (m Model) closeTab() (Model, tea.Cmd) {
	if len(m.tabs) <= 1 {
		m = m.saveTabs()
		return m, m.quit()
	}
	m.saveTab()
	m.denyPending()
	if sess := m.sess; sess != nil {
		sess.Cancel()
	}
	if m.stopPump != nil {
		m.stopPump()
	}
	i := m.cur
	m.park()
	m.tabs = slices.Delete(m.tabs, i, i+1)
	m.load(max(i-1, 0))
	return m, nil
}
