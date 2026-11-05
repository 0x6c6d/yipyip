package tui

import (
	"sync"
	"time"

	"yipyip/internal/session"
)

// saveState is the per-conversation save bookkeeping, shared (by pointer) by
// all copies of a Model: the time of the first assembly and the session id
// once the Store has derived it, so later saves overwrite the same file, plus
// the autosave state (see autosave.go). mu guards every field the autosave
// writer goroutine touches: id and the autosave fields; created is used on
// the UI goroutine only.
type saveState struct {
	created string

	mu           sync.Mutex
	id           string
	lastAutosave time.Time            // when the last autosave was requested
	inFlight     bool                 // an autosave command is issued or running
	started      bool                 // its writer goroutine has started
	wantAgain    bool                 // a turn-end autosave waits for the running one
	next         *session.SessionData // the data of that waiting autosave
	errShown     bool                 // the current failing streak was reported
	closed       bool                 // the exit save took over: no more autosaves
	done         chan struct{}        // closed when the in-flight autosave ends
}

// assembleSession builds the session data of the current tab from the agent
// session and the TUI's own state (the Store is pure storage). It reports
// false when there is nothing to save: no agent session or an empty
// conversation.
func (m Model) assembleSession() (*session.SessionData, bool) {
	if m.sess == nil {
		return nil, false
	}
	// The creation time is stamped once, on the first assembly that has
	// something to save.
	id, created := "", ""
	if m.saved != nil {
		id, created = m.saved.sessionID(), m.saved.created
	}
	if created == "" {
		created = m.now().UTC().Format(time.RFC3339)
	}
	// A turn still running at exit is cut by the exit: FromAgent stores it
	// as finished and interrupted, never resumable (A5).
	d, ok := session.FromAgent(m.sess, id, created)
	if !ok {
		return nil, false
	}
	if m.saved == nil {
		m.saved = &saveState{}
	}
	m.saved.created = created

	if m.hist != nil {
		d.History = append([]string{}, m.hist.entries...)
	}
	if m.pastes != nil {
		d.Pastes.N = m.pastes.n
		for ph, text := range m.pastes.texts {
			d.Pastes.Entries[ph] = text
		}
	}
	d.Queue = append([]string{}, m.queue...)
	return d, true
}

// saveTabs saves every open tab with a non-empty conversation before the
// program quits. A failure appends one "could not save session: ..." line to
// that tab's conversation; the quit goes on regardless.
func (m Model) saveTabs() Model {
	for i := range m.tabs {
		m.inTab(i, (*Model).saveTab)
	}
	return m
}

// saveTab saves the live tab (see inTab) through the Options.SaveOnExit seam or, by default,
// the session Store of the start directory.
//
// An autosave of the tab still in flight is waited for first (bounded), so
// the exit save overwrites the file it wrote: one file per conversation.
// After a successful save the retention prune runs (errors ignored).
func (m *Model) saveTab() {
	if m.saved != nil {
		m.saved.settle(exitSaveWait)
	}
	data, ok := m.assembleSession()
	if !ok {
		return
	}
	save := m.opts.SaveOnExit
	if save == nil {
		store, err := session.NewStore(m.opts.StartDir)
		if err != nil {
			m.add(blockError, "could not save session: "+m.redact(err.Error()))
			return
		}
		save = store.Save
	}
	if err := save(data); err != nil {
		m.add(blockError, "could not save session: "+m.redact(err.Error()))
		return
	}
	if m.saved != nil {
		m.saved.setID(data.ID)
	}
	_, prune := m.autosaveFuncs()
	_ = prune(keepSessions, protectIDs(m.saveStates(), data.ID)...) // a prune error is ignored
}
