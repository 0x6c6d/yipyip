package tui

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"yipyip/internal/agent"
	"yipyip/internal/session"
	"yipyip/internal/skills"
)

// Options configures the initial state of the UI.
type Options struct {
	StartDir string
	ModelID  string // "" = no model
	Effort   string // "" = "medium"
	Mode     string // "" = "manual"

	// Now is the clock of the UI (Ctrl+C window, turn clock); nil = time.Now.
	Now func() time.Time

	// Session is the agent session of the first tab. When set, model, effort
	// and mode come from it and the fields above are ignored.
	Session *agent.Session

	// NewSession builds the agent session of a new tab (/new); cmd/yip wires
	// it with the same config as Session. nil: a new tab cannot be opened
	// while Session is set ("could not open a new tab").
	NewSession func() (*agent.Session, error)

	// Clipboard copies already redacted plain text to the system clipboard;
	// nil = defaultClipboard (OSC 52 plus the platform tool).
	Clipboard func(string) error

	// SaveOnExit writes the session data of a tab on /exit, /quit and the
	// double Ctrl+C; nil = the session Store of StartDir. A test seam.
	SaveOnExit func(*session.SessionData) error

	// ResumeData is a saved session loaded by `yip --resume <id>`; New
	// restores it into Session (see Model.restore); nil = a fresh start.
	ResumeData *session.SessionData

	// ResumePick asks the UI to open the session picker at startup
	// (`yip --resume` without id, stdin a tty). With no saved session the
	// picker does not open and the start is a fresh session.
	ResumePick bool

	// StdinTTY reports whether stdin is a terminal (a character device); set
	// by cmd/yip, a plain value so tests never fake a tty.
	StdinTTY bool

	// SettingsPath is the user config file that every model, effort and
	// mode change is saved to (see settings.go); "" = never saved.
	SettingsPath string

	// SaveSettings writes the selection to the settings file; nil =
	// config.SaveSettings. A test seam.
	SaveSettings func(path, model, effort, mode string) error

	// Home is the user's home directory for the global skills folder;
	// "" = global skills are not read.
	Home string
}

// EventMsg carries one agent session event into Update. sess is the session
// that produced it: the event goes to that session's tab (nil = the current
// tab).
type EventMsg struct {
	Event agent.Event
	sess  *agent.Session
}

// ctrlCWindow is how long a first Ctrl+C stays armed.
const ctrlCWindow = 2 * time.Second

type blockKind int

const (
	blockPlain blockKind = iota
	blockAssistant
	blockError
	blockInfo
	blockTool
	blockMarker   // permanent "Worked for" / "Interrupted after" line
	blockSubagent // live subagent block; text is the subagent id
)

// block is one entry of the conversation. Text is kept raw and redacted when
// rendered, so a secret split across streamed deltas is still masked.
type block struct {
	kind blockKind
	text string
	call *toolCall // blockTool only
}

var (
	errorStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("1"))
	infoStyle  = lipgloss.NewStyle().Faint(true)
)

const (
	defaultWidth  = 80
	defaultHeight = 24
	inputRows     = 3 // text rows inside the border
	statusRows    = 2
	borderRows    = 2
	borderCols    = 2
)

// Model is the Bubble Tea model of the yipyip UI.
type Model struct {
	opts          Options
	skills        []skills.Skill // loaded by New and re-read by /skills, global for all tabs
	skillWarnings []string
	jevOn         bool // /jev switch, global for all tabs (never saved)
	viewport      viewport.Model
	input         textarea.Model
	width         int
	height        int

	ctx    context.Context // cancelled on quit: stops the blocking event command
	cancel context.CancelFunc

	now      func() time.Time
	ctrlCAt  time.Time    // time of the armed first Ctrl+C; zero when not armed
	picker   picker       // /model picker state
	resume   resumePicker // resume session picker state
	expanded bool         // Ctrl+O: tool results shown in full

	subInfo func() []agent.SubInfo // test seam for subagents(); nil = Session.Subagents()

	// tab is the live state of the current tab (conversation, session,
	// history, pastes, queue, turn state): m.blocks, m.sess, ... are the
	// current tab's. tabs holds every tab (1..maxTabs); its entry cur is
	// refreshed from tab whenever the list is used (see park), and the
	// slice is copy-on-write so Model copies never share an update.
	tab
	tabs []tab
	cur  int // index of the current tab in tabs

	// background is set while an event or save of a non-current tab is
	// applied (inTab): the viewport shows the current tab and is not redrawn.
	background bool

	clip func(string) error // clipboard writer: Options.Clipboard or defaultClipboard

	lines       []string  // rendered (redacted) conversation lines, as given to the viewport
	sel         selection // left-button drag over the conversation
	inSel       selection // left-button drag over the input (cells: visual row, rune column)
	notice      string    // copy notice, shown until noticeUntil
	noticeUntil time.Time
}

// New returns a Model with the input focused and a default 80x24 layout.
func New(opts Options) Model {
	if opts.Effort == "" {
		opts.Effort = "medium"
	}
	if opts.Mode == "" {
		opts.Mode = "manual"
	}

	ta := textarea.New()
	ta.Prompt = ""
	ta.Placeholder = "Type a message, /help for commands"
	ta.ShowLineNumbers = false
	ta.CharLimit = 0
	ta.KeyMap.InsertNewline = key.NewBinding(
		key.WithKeys("alt+enter"),
		key.WithHelp("alt+enter", "newline"),
	)
	ta.Focus()

	ctx, cancel := context.WithCancel(context.Background())
	now := opts.Now
	if now == nil {
		now = time.Now
	}
	clip := opts.Clipboard
	if clip == nil {
		clip = defaultClipboard
	}
	m := Model{
		opts:     opts,
		viewport: viewport.New(defaultWidth, 1),
		input:    ta,
		ctx:      ctx,
		cancel:   cancel,
		now:      now,
		tab:      newTab(ctx, opts.Session),
		clip:     clip,
	}
	m.tabs = []tab{m.tab}
	if opts.StartDir != "" {
		m.skills, m.skillWarnings = skills.Load(opts.Home, opts.StartDir)
	}
	m.resize(defaultWidth, defaultHeight)
	switch {
	case opts.ResumeData != nil:
		m.restore(0, opts.ResumeData)
	case opts.ResumePick:
		m.openResumePicker()
	}
	m.addSkillWarnings()
	return m
}

// Init implements tea.Model.
func (m Model) Init() tea.Cmd {
	cmds := []tea.Cmd{textarea.Blink}
	for i := range m.tabs {
		cmds = append(cmds, waitEventOf(m.tabAt(i)))
	}
	return tea.Batch(cmds...)
}

// waitEvent returns a command that blocks on the current tab session's next
// event. It ends (returning nil) when the model's context is cancelled on
// quit or the tab is closed.
func (m Model) waitEvent() tea.Cmd { return waitEventOf(m.tab) }

// waitEventOf is waitEvent for tab t.
func waitEventOf(t tab) tea.Cmd {
	sess, ctx := t.sess, t.pump
	if sess == nil || ctx == nil {
		return nil
	}
	return func() tea.Msg {
		ev, err := sess.NextEvent(ctx)
		if err != nil {
			return nil
		}
		return EventMsg{Event: ev, sess: sess}
	}
}

// quit cancels the model's context and returns the quit command.
func (m Model) quit() tea.Cmd {
	m.cancel()
	return tea.Quit
}

// Update implements tea.Model. It always returns a tui.Model value.
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.resize(msg.Width, msg.Height)
		return m, nil
	case EventMsg:
		if msg.sess == nil {
			cmd := m.apply(msg.Event)
			return m, tea.Batch(cmd, m.waitEvent())
		}
		i := m.tabOf(msg.sess)
		if i < 0 {
			return m, nil // the tab was closed: its pump ends
		}
		var cmd tea.Cmd
		m.inTab(i, func(t *Model) { cmd = t.apply(msg.Event) })
		return m, tea.Batch(cmd, waitEventOf(m.tabAt(i)))
	case tickMsg:
		if m.anySubRunning() {
			m.refresh() // elapsed times of running subagent blocks
		}
		if !m.anyTurnActive() {
			return m, nil
		}
		return m, tickCmd()
	case noticeMsg:
		return m, nil // redraw: the copy notice has expired
	case compactDoneMsg:
		i := m.cur
		if msg.sess != nil {
			i = m.tabOf(msg.sess)
		}
		m.inTab(i, func(t *Model) { t.compactDone(msg.err) })
		return m, nil
	case tea.KeyMsg, tea.MouseMsg:
		if k, isKey := msg.(tea.KeyMsg); isKey && m.resume.open && k.String() != "ctrl+c" {
			return m.resumeKey(k)
		}
		if k, isKey := msg.(tea.KeyMsg); isKey && len(m.pending) > 0 && k.String() != "ctrl+c" {
			return m.approvalKey(k)
		}
		if k, isKey := msg.(tea.KeyMsg); isKey && len(m.questions) > 0 && k.String() != "ctrl+c" {
			return m.questionKey(k)
		}
		if k, isKey := msg.(tea.KeyMsg); m.picker.open && !(isKey && k.String() == "ctrl+c") {
			return m.pickerKey(msg)
		}
		if mm, isMouse := msg.(tea.MouseMsg); isMouse {
			return m.mouse(mm)
		}
		if nm, ok := m.paste(msg.(tea.KeyMsg)); ok {
			return nm, nil
		}
		ks := msg.(tea.KeyMsg).String()
		if ignoredTabKey(ks) {
			return m, nil
		}
		switch ks {
		case "ctrl+c":
			return m.ctrlC()
		case "esc":
			if sess := m.sess; sess != nil && sess.Busy() {
				m.interrupted = m.interrupted || m.turnActive
				sess.Cancel()
			}
			if m.busy() && len(m.queue) > 0 {
				m.restoreQueue()
			}
			return m, nil
		case "enter":
			return m.submit()
		case "ctrl+o":
			m.expanded = !m.expanded
			m.refresh()
			return m, nil
		case "pgup", "pgdown":
			var cmd tea.Cmd
			m.viewport, cmd = m.viewport.Update(msg)
			return m, cmd
		case "ctrl+home":
			m.viewport.GotoTop()
			return m, nil
		case "ctrl+end":
			m.viewport.GotoBottom()
			return m, nil
		case "home":
			if m.input.Value() == "" {
				m.viewport.GotoTop()
				return m, nil
			}
		case "end":
			if m.input.Value() == "" {
				m.viewport.GotoBottom()
				return m, nil
			}
		case "shift+tab":
			prev := m.currentSel()
			m.toggleMode()
			m.saveSettingsIfChanged(prev)
			return m, nil
		case "ctrl+t":
			return m.openTab()
		case "ctrl+w":
			return m.closeTab()
		case "tab":
			// On a command name the v1 completion (longest common prefix)
			// wins; arguments are completed from the ghost text.
			if len(m.completions()) > 0 {
				return m.completeInput(), nil
			}
			if nm, ok := m.acceptSuggestion(); ok {
				return nm, nil
			}
		case "right":
			if nm, ok := m.acceptSuggestion(); ok {
				return nm, nil
			}
		case "up":
			if m.input.Value() == "" && len(m.queue) > 0 {
				m.restoreQueue()
				return m, nil
			}
			if nm, ok := m.historyUp(); ok {
				return nm, nil
			}
		case "down":
			if nm, ok := m.historyDown(); ok {
				return nm, nil
			}
		}
	}

	var cmd tea.Cmd
	// Only PgUp/PgDn (handled above) scroll the viewport: its other default
	// keys (space, f, b, j, k, ...) are ordinary typing here.
	m.input, cmd = m.input.Update(msg)
	return m, cmd
}

func (m Model) ctrlC() (tea.Model, tea.Cmd) {
	m.denyPending()
	m.cancelQuestions()
	if sess := m.sess; sess != nil && sess.Busy() {
		m.interrupted = m.interrupted || m.turnActive
		sess.Cancel()
	}
	if m.busy() && len(m.queue) > 0 {
		m.restoreQueue()
	}
	now := m.now()
	if !m.ctrlCAt.IsZero() && now.Sub(m.ctrlCAt) <= ctrlCWindow {
		m = m.saveTabs()
		return m, m.quit()
	}
	m.ctrlCAt = now
	m.add(blockInfo, "press Ctrl+C again to quit")
	return m, nil
}

func (m Model) submit() (tea.Model, tea.Cmd) {
	text := strings.TrimSpace(m.input.Value())
	if text == "" {
		return m, nil
	}
	m.input.Reset()
	if m.rmPending != "" {
		// The reply to a /sessions rm confirmation: neither history, nor
		// queue, nor the model.
		return m.Command(text)
	}
	if m.hist != nil {
		m.hist.push(text)
	}
	if !strings.HasPrefix(text, "/") && m.busy() {
		m.enqueue(text)
		return m, nil
	}
	return m.Command(text)
}

// Command handles one submitted input line as if it had been typed followed
// by Enter: a slash command, or a message for the model.
func (m Model) Command(line string) (Model, tea.Cmd) {
	text := strings.TrimSpace(line)
	if text == "" {
		return m, nil
	}
	if m.rmPending != "" {
		return m.answerRm(text), nil
	}
	if strings.HasPrefix(text, "/") {
		return m.runSlash(text)
	}
	return m.sendUser(text, m.pastes.expand(text))
}

// sendUser shows shown as the typed line and sends msg to the session as a
// user message, starting a turn; a missing model, a busy session and errors
// become one transcript line.
func (m Model) sendUser(shown, msg string) (Model, tea.Cmd) {
	start := len(m.blocks)
	m.add(blockPlain, "> "+shown)
	sess := m.sess
	if sess == nil {
		m.add(blockInfo, "no model configured")
		return m, nil
	}
	switch err := sess.Submit(msg); {
	case err == nil:
		m.pushTurnStart(start)
		return m, m.startTurn()
	case errors.Is(err, agent.ErrNoModel):
		m.add(blockInfo, "no model configured")
	case errors.Is(err, agent.ErrBusy):
		m.add(blockInfo, busyLine)
	default:
		m.add(blockError, "error: "+err.Error())
	}
	return m, nil
}

// Drain applies all queued session events of every tab without blocking,
// each to the tab of its session. Commands returned by them (the tick of a
// turn started from the message queue) are dropped.
func (m Model) Drain() Model {
	for i := range m.tabs {
		sess := m.tabAt(i).sess
		if sess == nil {
			continue
		}
		for _, ev := range sess.PollEvents() {
			m.inTab(i, func(t *Model) { t.apply(ev) })
		}
	}
	return m
}

// anyTurnActive reports whether a turn runs in any tab (the clock ticks on).
func (m *Model) anyTurnActive() bool {
	for i := range m.tabs {
		if m.tabAt(i).turnActive {
			return true
		}
	}
	return false
}

// apply renders one session event. It returns the tick command of a turn
// started from the message queue at the end of the previous one, else nil.
func (m *Model) apply(ev agent.Event) tea.Cmd {
	if isMainAgent(ev.AgentID) {
		switch ev.Kind {
		case agent.EvText, agent.EvToolCall, agent.EvToolResult, agent.EvError, agent.EvTurnDone:
			m.retryStatus = "" // the retry wait is over
		}
	}
	switch ev.Kind {
	case agent.EvRetry:
		if isMainAgent(ev.AgentID) && ev.Text != "" {
			m.retryStatus = ev.Text // "retrying (n/max) in <wait>", built by the agent
		}
		return nil
	case agent.EvText:
		if m.streaming && len(m.blocks) > 0 {
			m.blocks[len(m.blocks)-1].text += ev.Text
			m.refresh()
			return nil
		}
		m.add(blockAssistant, ev.Text)
		m.streaming = true
	case agent.EvToolCall:
		if !isMainAgent(ev.AgentID) {
			m.subToolCall(ev)
			return nil
		}
		m.openCalls++
		if ev.Tool == "spawn_agent" {
			return nil // shown as the subagent's live block
		}
		m.addToolCall(ev)
	case agent.EvToolResult:
		if !isMainAgent(ev.AgentID) {
			m.ensureSub(ev.AgentID)
			return nil
		}
		if m.openCalls > 0 {
			m.openCalls--
		}
		if ev.Tool == "spawn_agent" {
			return nil
		}
		m.addToolResult(ev)
	case agent.EvApproval:
		if ev.Approval != nil {
			m.enqueueApproval(ev.Approval)
		}
	case agent.EvQuestion:
		if ev.Question != nil {
			m.enqueueQuestion(ev.Question)
		}
	case agent.EvError:
		m.add(blockError, "error: "+ev.Text)
	case agent.EvInfo:
		m.add(blockInfo, ev.Text)
	case agent.EvSubagent:
		if isMainAgent(ev.AgentID) {
			m.refresh()
			return nil
		}
		m.subStatusEvent(ev.AgentID)
	case agent.EvTurnDone:
		if m.turnActive && isMainAgent(ev.AgentID) {
			verb := "✻ Worked for "
			if m.interrupted {
				verb = "✻ Interrupted after "
			}
			m.add(blockMarker, verb+formatElapsed(m.now().Sub(m.turnStart)))
		}
		interrupted := m.interrupted
		m.turnActive = false
		m.interrupted = false
		m.openCalls = 0
		m.streaming = false
		m.closeToolCalls()
		m.pending = nil // stale: the turn is over, the gate no longer waits
		m.cancelQuestions()
		if isMainAgent(ev.AgentID) && !interrupted {
			return m.sendQueue()
		}
	}
	return nil
}

// add appends a block of the given kind and ends any streaming assistant block.
func (m *Model) add(kind blockKind, text string) {
	m.blocks = append(m.blocks, block{kind: kind, text: text})
	m.streaming = false
	m.refresh()
}

func (m *Model) redact(s string) string {
	if sess := m.sess; sess != nil {
		return sess.Redact(s)
	}
	return s
}

// refresh re-renders the blocks into the viewport and scrolls to the bottom.
func (m *Model) refresh() { m.refreshTo(m.viewport.AtBottom()) }

// refreshTo re-renders the blocks and, below them, the box of the pending
// question; with follow set the viewport ends at the bottom, otherwise the
// scroll position is kept.
func (m *Model) refreshTo(follow bool) {
	if m.background {
		return // a non-current tab: rendered when it becomes current
	}
	parts := make([]string, 0, len(m.blocks))
	for _, b := range m.blocks {
		if r := m.renderBlock(b, m.viewport.Width); r != "" {
			parts = append(parts, r)
		}
	}
	if len(m.questions) > 0 {
		parts = append(parts, m.questionBox())
	}
	content := lipgloss.NewStyle().Width(m.viewport.Width).Render(strings.Join(parts, "\n\n"))
	m.lines = strings.Split(content, "\n")
	m.viewport.SetContent(content)
	if follow {
		m.viewport.GotoBottom()
	}
}

func (m *Model) resize(w, h int) {
	if w < borderCols+1 {
		w = borderCols + 1
	}
	if h < 1 {
		h = 1
	}
	follow := m.viewport.AtBottom()
	m.width, m.height = w, h
	m.input.SetWidth(w - borderCols)
	m.input.SetHeight(inputRows)
	vh := h - inputRows - borderRows - statusRows - tabBarRows
	if vh < 1 {
		vh = 1
	}
	m.viewport.Width = w
	m.viewport.Height = vh
	m.refreshTo(follow)
}

// View implements tea.Model.
func (m Model) View() string {
	box := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		Render(m.withGhost(m.inputView()))
	vp := m.overlays(m.viewportView())
	row1, row2 := m.StatusRows()
	return lipgloss.JoinVertical(lipgloss.Left, m.tabBar(), vp, box, row1, row2)
}

// overlays puts the approval prompt, picker or completions, the subagent
// panel, the busy line, the queue block and the copy notice over the
// viewport view vp. The height stays that of vp.
func (m Model) overlays(vp string) string {
	if len(m.pending) > 0 {
		vp = m.overlayApproval(vp)
	} else if m.resume.open {
		vp = m.overlayResume(vp)
	} else if m.picker.open {
		vp = m.overlayPicker(vp)
	} else if matches := m.completions(); len(matches) > 0 {
		// The completion line takes the place of the top viewport line.
		if _, rest, ok := strings.Cut(vp, "\n"); ok {
			vp = rest
		}
		vp = lipgloss.JoinVertical(lipgloss.Left, vp, infoStyle.Render(m.completionLine(matches)))
	}
	vp = m.withPanel(vp)
	vp = m.withThinking(vp)
	vp = m.withQueue(vp)
	return m.withNotice(vp)
}
