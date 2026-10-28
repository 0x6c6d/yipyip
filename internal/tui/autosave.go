package tui

import (
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"yipyip/internal/agent"
	"yipyip/internal/session"
)

const (
	// autosaveSpacing is the least time between two checkpoint autosaves of
	// one tab; a turn-end autosave is never delayed by it.
	autosaveSpacing = time.Minute

	// keepSessions is how many session files the retention prune keeps
	// (besides the protected ones of the open tabs).
	keepSessions = 25

	// exitSaveWait bounds how long an exit save waits for the tab's
	// autosave still in flight.
	exitSaveWait = 5 * time.Second
)

// autosaveMsg reports the end of a tab's autosave writer to Update: state
// identifies the tab, errs holds the failures to show (only the first of a
// failing streak), still unredacted.
type autosaveMsg struct {
	state *saveState
	errs  []error
}

// sessionID returns the id the Store derived for the conversation ("" before
// the first successful save).
func (s *saveState) sessionID() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.id
}

// setID stores the id of a saved conversation.
func (s *saveState) setID(id string) {
	s.mu.Lock()
	s.id = id
	s.mu.Unlock()
}

// autosaveFuncs returns the save and prune functions of autosaves and exit
// prunes: the Options seams or, by default, the session Store of StartDir
// (opened on use, in the caller's goroutine).
func (m *Model) autosaveFuncs() (save func(*session.SessionData) error, prune func(int, ...string) error) {
	startDir := m.opts.StartDir
	save, prune = m.opts.Autosave, m.opts.PruneSessions
	if save == nil {
		save = func(d *session.SessionData) error {
			st, err := session.NewStore(startDir)
			if err != nil {
				return err
			}
			return st.Save(d)
		}
	}
	if prune == nil {
		prune = func(keep int, protect ...string) error {
			st, err := session.NewStore(startDir)
			if err != nil {
				return err
			}
			return st.Prune(keep, protect...)
		}
	}
	return save, prune
}

// saveStates returns the save states of all open tabs (for the prune's
// protected ids).
func (m *Model) saveStates() []*saveState {
	var out []*saveState
	for i := range m.tabs {
		if s := m.tabAt(i).saved; s != nil {
			out = append(out, s)
		}
	}
	return out
}

// protectIDs returns the session ids of states plus saved, without empties.
// It locks each state on its own, never two at once.
func protectIDs(states []*saveState, saved string) []string {
	var ids []string
	for _, s := range states {
		if id := s.sessionID(); id != "" {
			ids = append(ids, id)
		}
	}
	if saved != "" {
		ids = append(ids, saved)
	}
	return ids
}

// autosaveFor decides on the UI goroutine whether ev triggers an autosave of
// the live tab: a main-agent turn end always, a main-agent checkpoint once
// autosaveSpacing has passed since the tab's last autosave (or turn start).
// The data is assembled here; the write runs in the returned command, never
// on the UI goroutine. With a write already in flight a turn-end request is
// queued behind it (wantAgain) and nil is returned.
func (m *Model) autosaveFor(ev agent.Event) tea.Cmd {
	if !isMainAgent(ev.AgentID) {
		return nil
	}
	turnEnd := ev.Kind == agent.EvTurnDone
	if !turnEnd && ev.Kind != agent.EvCheckpoint {
		return nil
	}
	if m.saved == nil {
		m.saved = &saveState{}
	}
	s := m.saved
	now := m.now()
	s.mu.Lock()
	last, closed, inFlight := s.lastAutosave, s.closed, s.inFlight
	s.mu.Unlock()
	if closed {
		return nil
	}
	if !turnEnd {
		if inFlight {
			return nil
		}
		ref := last
		if ref.IsZero() {
			ref = m.turnStart
		}
		if !ref.IsZero() && now.Sub(ref) < autosaveSpacing {
			return nil
		}
	}
	data, ok := m.assembleSession()
	if !ok {
		return nil
	}
	if turnEnd {
		// The turn is over even if the session has not yet cleared its
		// busy flag: interrupted only when the user cut it (Esc, Ctrl+C).
		data.Turn.Interrupted = m.interrupted
	}
	save, prune := m.autosaveFuncs()
	states := m.saveStates()

	s.mu.Lock()
	defer s.mu.Unlock()
	s.lastAutosave = now
	if s.inFlight {
		s.wantAgain, s.next = true, data
		return nil
	}
	s.inFlight, s.started, s.done = true, false, make(chan struct{})
	return func() tea.Msg { return s.write(data, save, prune, states) }
}

// write is the autosave writer of one tab (run in a command goroutine): it
// saves data, stores the Store's id, prunes after a success and then saves
// the queued request, if any, until none is left. It does nothing when an
// exit save has taken over before it started.
func (s *saveState) write(data *session.SessionData, save func(*session.SessionData) error, prune func(int, ...string) error, states []*saveState) tea.Msg {
	s.mu.Lock()
	if s.closed || !s.inFlight || s.started {
		s.mu.Unlock()
		return nil
	}
	s.started = true
	s.mu.Unlock()

	var errs []error
	for {
		before := data.ID
		err := save(data)
		s.mu.Lock()
		if err == nil {
			// A restore may have switched the tab to another conversation
			// meanwhile: keep its id then.
			if s.id == before {
				s.id = data.ID
			}
			s.errShown = false
		} else if !s.errShown {
			s.errShown = true
			errs = append(errs, err)
		}
		s.mu.Unlock()
		if err == nil {
			_ = prune(keepSessions, protectIDs(states, data.ID)...) // a prune error is ignored
		}

		s.mu.Lock()
		if s.wantAgain && s.next != nil && !s.closed {
			data, s.next, s.wantAgain = s.next, nil, false
			data.ID = s.id
			s.mu.Unlock()
			continue
		}
		s.inFlight, s.started, s.wantAgain, s.next = false, false, false, nil
		close(s.done)
		s.mu.Unlock()
		break
	}
	if len(errs) == 0 {
		return nil
	}
	return autosaveMsg{state: s, errs: errs}
}

// settle prepares the exit save of the tab: an autosave that has not started
// yet is dropped, one that is running is waited for (at most wait), and
// later autosaves of the tab are refused.
func (s *saveState) settle(wait time.Duration) {
	s.mu.Lock()
	s.closed = true
	s.wantAgain, s.next = false, nil
	if s.inFlight && !s.started {
		s.inFlight = false
		close(s.done)
	}
	running, done := s.inFlight, s.done
	s.mu.Unlock()
	if !running {
		return
	}
	t := time.NewTimer(wait)
	defer t.Stop()
	select {
	case <-done:
	case <-t.C:
	}
}

// autosaveDone shows the autosave failures of msg in the tab they belong to,
// redacted, one "could not autosave session: ..." line each.
func (m *Model) autosaveDone(msg autosaveMsg) {
	for i := range m.tabs {
		if m.tabAt(i).saved != msg.state {
			continue
		}
		m.inTab(i, func(t *Model) {
			for _, err := range msg.errs {
				t.add(blockError, "could not autosave session: "+t.redact(err.Error()))
			}
		})
		return
	}
}
