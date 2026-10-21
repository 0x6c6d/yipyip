package tui

import (
	"errors"
	"fmt"
	"io/fs"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"yipyip/internal/session"
)

// sessionsMaxRows is the most rows `/sessions` lists before "… +N more".
const sessionsMaxRows = 10

var sessionsDim = lipgloss.NewStyle().Foreground(lipgloss.Color("244"))

// cmdSessions is `/sessions`: list (no argument or `ls`), `<id>` loads a
// session into the current tab, `rm <id>` removes one after a y/n reply.
func cmdSessions(m Model, args string) (Model, tea.Cmd) {
	store, err := session.NewStore(m.opts.StartDir)
	if err != nil {
		m.add(blockError, "could not open sessions: "+m.redact(err.Error()))
		return m, nil
	}
	fields := strings.Fields(args)
	switch {
	case len(fields) == 0 || (len(fields) == 1 && fields[0] == "ls"):
		m.sessionsList(store)
	case fields[0] == "rm":
		if len(fields) != 2 {
			m.add(blockInfo, "usage: /sessions rm <id>")
			break
		}
		m.sessionsRm(store, fields[1])
	case len(fields) == 1:
		m.sessionsLoad(store, fields[0])
	default:
		m.add(blockInfo, "usage: /sessions [ls | <id> | rm <id>]")
	}
	return m, nil
}

// sessionRow formats one list row: id, title, time, tokens and model, two
// spaces between the columns; id and time are dim.
func (m *Model) sessionRow(d session.SessionData) string {
	title := strings.Join(strings.Fields(m.redact(d.Title)), " ")
	if title == "" {
		title = "(untitled)"
	}
	stamp := d.Updated
	if stamp == "" {
		stamp = d.Created
	}
	when := "unknown"
	if t, err := time.Parse(time.RFC3339, stamp); err == nil {
		when = t.Local().Format("15:04  Jan 2")
	}
	return strings.Join([]string{
		sessionsDim.Render(d.ID),
		title,
		sessionsDim.Render(when),
		fmt.Sprintf("%d toks", d.TotalTokens),
		m.redact(d.Model),
	}, "  ")
}

func (m *Model) sessionsList(store *session.Store) {
	list, err := store.List()
	if err != nil {
		m.add(blockError, "could not list sessions: "+m.redact(err.Error()))
		return
	}
	if len(list) == 0 {
		m.add(blockInfo, "no saved sessions")
		return
	}
	var rows []string
	for i, d := range list {
		if i == sessionsMaxRows {
			rows = append(rows, fmt.Sprintf("… +%d more", len(list)-sessionsMaxRows))
			break
		}
		rows = append(rows, m.sessionRow(d))
	}
	m.add(blockInfo, strings.Join(rows, "\n"))
}

// sessionsLoad replaces the current tab's conversation with the stored one
// through the same restore path as `--resume`.
func (m *Model) sessionsLoad(store *session.Store, id string) {
	data, err := store.Load(id)
	if err != nil {
		m.sessionsLoadFailed(id, err)
		return
	}
	if m.busy() {
		m.add(blockInfo, busyLine)
		return
	}
	m.restore(m.cur, data)
	m.add(blockInfo, "loaded session "+id)
}

func (m *Model) sessionsLoadFailed(id string, err error) {
	if errors.Is(err, fs.ErrNotExist) || !isSessionID(id) {
		m.add(blockInfo, "unknown session "+m.redact(id))
		return
	}
	m.add(blockInfo, "could not load session "+id)
}

// isSessionID reports whether id has the shape of a session id (12 lowercase
// hex chars).
func isSessionID(id string) bool {
	if len(id) != 12 {
		return false
	}
	for _, c := range id {
		if !strings.ContainsRune("0123456789abcdef", c) {
			return false
		}
	}
	return true
}

// sessionsRm starts the confirmation of removing session id.
func (m *Model) sessionsRm(store *session.Store, id string) {
	if m.saved != nil && m.saved.id != "" && m.saved.id == id {
		m.add(blockInfo, "cannot remove the current session")
		return
	}
	if _, err := store.Load(id); err != nil && (errors.Is(err, fs.ErrNotExist) || !isSessionID(id)) {
		m.add(blockInfo, "unknown session "+m.redact(id))
		return
	}
	m.rmPending = id
	m.add(blockInfo, "remove session "+id+"? y/n")
}

// answerRm handles the line typed after a `/sessions rm` prompt: `y` removes
// the session, anything else keeps it. The pending state always clears.
func (m Model) answerRm(reply string) Model {
	id := m.rmPending
	m.rmPending = ""
	if m.saved != nil && m.saved.id == id {
		m.add(blockInfo, "cannot remove the current session")
		return m
	}
	if reply != "y" {
		m.add(blockInfo, "kept session "+id)
		return m
	}
	store, err := session.NewStore(m.opts.StartDir)
	if err == nil {
		err = store.Delete(id)
	}
	switch {
	case err == nil:
		m.add(blockInfo, "removed session "+id)
	case errors.Is(err, fs.ErrNotExist):
		m.add(blockInfo, "unknown session "+id)
	default:
		m.add(blockInfo, "could not remove session "+id)
	}
	return m
}
