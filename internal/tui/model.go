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
)

// Options configures the initial state of the UI.
type Options struct {
	StartDir string
	ModelID  string // "" = no model
	Effort   string // "" = "medium"
	Mode     string // "" = "manual"

	// Now is the clock of the UI (Ctrl+C window, turn clock); nil = time.Now.
	Now func() time.Time

	// Session is the agent session. When set, model, effort and mode come
	// from it and the fields above are ignored.
	Session *agent.Session
}

// EventMsg carries one agent session event into Update.
type EventMsg struct{ Event agent.Event }

// ctrlCWindow is how long a first Ctrl+C stays armed.
const ctrlCWindow = 2 * time.Second

type blockKind int

const (
	blockPlain blockKind = iota
	blockAssistant
	blockError
	blockInfo
	blockTool
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
	opts     Options
	viewport viewport.Model
	input    textarea.Model
	blocks   []block
	width    int
	height   int

	ctx    context.Context // cancelled on quit: stops the blocking event command
	cancel context.CancelFunc

	now       func() time.Time
	ctrlCAt   time.Time                // time of the armed first Ctrl+C; zero when not armed
	streaming bool                     // the last block is an assistant block still receiving deltas
	picker    picker                   // /model picker state
	pending   []*agent.ApprovalRequest // approval requests waiting for y/n/a, FIFO
	expanded  bool                     // Ctrl+O: tool results shown in full

	turnActive bool      // a turn is running: the Thinking line is shown and ticks go on
	turnStart  time.Time // when the running turn was submitted
	turnOut0   int       // session output tokens at submit
	openCalls  int       // main-agent tool calls without a result yet

	turnStarts []int // block index where each undoable submitted turn starts (copy-on-write)
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
	m := Model{
		opts:     opts,
		viewport: viewport.New(defaultWidth, 1),
		input:    ta,
		ctx:      ctx,
		cancel:   cancel,
		now:      now,
	}
	m.resize(defaultWidth, defaultHeight)
	return m
}

// Init implements tea.Model.
func (m Model) Init() tea.Cmd {
	return tea.Batch(textarea.Blink, m.waitEvent())
}

// waitEvent returns a command that blocks on the session's next event. It
// ends (returning nil) when the model's context is cancelled on quit.
func (m Model) waitEvent() tea.Cmd {
	sess, ctx := m.opts.Session, m.ctx
	if sess == nil {
		return nil
	}
	return func() tea.Msg {
		ev, err := sess.NextEvent(ctx)
		if err != nil {
			return nil
		}
		return EventMsg{Event: ev}
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
		m.apply(msg.Event)
		return m, m.waitEvent()
	case tickMsg:
		if !m.turnActive {
			return m, nil
		}
		return m, tickCmd()
	case compactDoneMsg:
		m.compactDone(msg.err)
		return m, nil
	case tea.KeyMsg, tea.MouseMsg:
		if k, isKey := msg.(tea.KeyMsg); isKey && len(m.pending) > 0 && k.String() != "ctrl+c" {
			return m.approvalKey(k)
		}
		if k, isKey := msg.(tea.KeyMsg); m.picker.open && !(isKey && k.String() == "ctrl+c") {
			return m.pickerKey(msg)
		}
		if _, isKey := msg.(tea.KeyMsg); !isKey {
			break
		}
		switch msg.(tea.KeyMsg).String() {
		case "ctrl+c":
			return m.ctrlC()
		case "esc":
			if sess := m.opts.Session; sess != nil && sess.Busy() {
				sess.Cancel()
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
		case "shift+tab":
			m.toggleMode()
			return m, nil
		case "tab":
			if len(m.completions()) > 0 {
				return m.completeInput(), nil
			}
		}
	}

	var cmd tea.Cmd
	if _, isMouse := msg.(tea.MouseMsg); isMouse {
		m.viewport, cmd = m.viewport.Update(msg)
		return m, cmd
	}
	// Only PgUp/PgDn (handled above) scroll the viewport: its other default
	// keys (space, f, b, j, k, ...) are ordinary typing here.
	m.input, cmd = m.input.Update(msg)
	return m, cmd
}

func (m Model) ctrlC() (tea.Model, tea.Cmd) {
	m.denyPending()
	if sess := m.opts.Session; sess != nil && sess.Busy() {
		sess.Cancel()
	}
	now := m.now()
	if !m.ctrlCAt.IsZero() && now.Sub(m.ctrlCAt) <= ctrlCWindow {
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
	return m.Command(text)
}

// Command handles one submitted input line as if it had been typed followed
// by Enter: a slash command, or a message for the model.
func (m Model) Command(line string) (Model, tea.Cmd) {
	text := strings.TrimSpace(line)
	if text == "" {
		return m, nil
	}
	if strings.HasPrefix(text, "/") {
		return m.runSlash(text)
	}
	start := len(m.blocks)
	m.add(blockPlain, "> "+text)
	sess := m.opts.Session
	if sess == nil {
		m.add(blockInfo, "no model configured")
		return m, nil
	}
	switch err := sess.Submit(text); {
	case err == nil:
		m.pushTurnStart(start)
		return m, m.startTurn()
	case errors.Is(err, agent.ErrNoModel):
		m.add(blockInfo, "no model configured")
	case errors.Is(err, agent.ErrBusy):
		m.add(blockInfo, "busy: press Esc to cancel the running turn")
	default:
		m.add(blockError, "error: "+err.Error())
	}
	return m, nil
}

// Drain applies all queued session events without blocking.
func (m Model) Drain() Model {
	if m.opts.Session == nil {
		return m
	}
	for _, ev := range m.opts.Session.PollEvents() {
		m.apply(ev)
	}
	return m
}

// apply renders one session event.
func (m *Model) apply(ev agent.Event) {
	switch ev.Kind {
	case agent.EvText:
		if m.streaming && len(m.blocks) > 0 {
			m.blocks[len(m.blocks)-1].text += ev.Text
			m.refresh()
			return
		}
		m.add(blockAssistant, ev.Text)
		m.streaming = true
	case agent.EvToolCall:
		if isMainAgent(ev.AgentID) {
			m.openCalls++
		}
		m.addToolCall(ev)
	case agent.EvToolResult:
		if isMainAgent(ev.AgentID) && m.openCalls > 0 {
			m.openCalls--
		}
		m.addToolResult(ev)
	case agent.EvApproval:
		if ev.Approval != nil {
			m.enqueueApproval(ev.Approval)
		}
	case agent.EvError:
		m.add(blockError, "error: "+ev.Text)
	case agent.EvInfo:
		m.add(blockInfo, ev.Text)
	case agent.EvSubagent:
		// Status changes only trigger a re-render: the panel reads
		// Subagents() live, so nothing is printed into the conversation.
		m.refresh()
	case agent.EvTurnDone:
		m.turnActive = false
		m.openCalls = 0
		m.streaming = false
		m.closeToolCalls()
		m.pending = nil // stale: the turn is over, the gate no longer waits
	}
}

// add appends a block of the given kind and ends any streaming assistant block.
func (m *Model) add(kind blockKind, text string) {
	m.blocks = append(m.blocks, block{kind: kind, text: text})
	m.streaming = false
	m.refresh()
}

func (m *Model) redact(s string) string {
	if sess := m.opts.Session; sess != nil {
		return sess.Redact(s)
	}
	return s
}

// refresh re-renders the blocks into the viewport and scrolls to the bottom.
func (m *Model) refresh() { m.refreshTo(m.viewport.AtBottom()) }

// refreshTo re-renders the blocks; with follow set the viewport ends at the
// bottom, otherwise the scroll position is kept.
func (m *Model) refreshTo(follow bool) {
	parts := make([]string, 0, len(m.blocks))
	for _, b := range m.blocks {
		if r := m.renderBlock(b, m.viewport.Width); r != "" {
			parts = append(parts, r)
		}
	}
	content := strings.Join(parts, "\n\n")
	m.viewport.SetContent(lipgloss.NewStyle().Width(m.viewport.Width).Render(content))
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
	vh := h - inputRows - borderRows - statusRows
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
		Render(m.input.View())
	vp := m.viewport.View()
	if len(m.pending) > 0 {
		vp = m.overlayApproval(vp)
	} else if m.picker.open {
		vp = m.overlayPicker(vp)
	} else if matches := m.completions(); len(matches) > 0 {
		// The completion line takes the place of the top viewport line.
		if _, rest, ok := strings.Cut(vp, "\n"); ok {
			vp = rest
		}
		vp = lipgloss.JoinVertical(lipgloss.Left, vp, infoStyle.Render(strings.Join(matches, " ")))
	}
	vp = m.withPanel(vp)
	vp = m.withThinking(vp)
	row1, row2 := m.StatusRows()
	return lipgloss.JoinVertical(lipgloss.Left, vp, box, row1, row2)
}
