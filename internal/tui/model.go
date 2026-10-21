package tui

import (
	"context"
	"errors"
	"fmt"
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
)

// block is one entry of the conversation. Text is kept raw and redacted when
// rendered, so a secret split across streamed deltas is still masked.
type block struct {
	kind blockKind
	text string
}

var (
	errorStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("1"))
	infoStyle  = lipgloss.NewStyle().Faint(true)
)

// commandFunc handles one slash command; args is the text after the name.
type commandFunc func(m Model, args string) (Model, tea.Cmd)

// slashCommands is the dispatcher table of the slash commands.
var slashCommands = map[string]commandFunc{
	"/exit": cmdQuit,
	"/quit": cmdQuit,
}

func cmdQuit(m Model, _ string) (Model, tea.Cmd) { return m, m.quit() }

const (
	defaultWidth  = 80
	defaultHeight = 24
	inputRows     = 3 // text rows inside the border
	statusRows    = 1
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
	ctrlCAt   time.Time // time of the armed first Ctrl+C; zero when not armed
	streaming bool      // the last block is an assistant block still receiving deltas
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
	m := Model{
		opts:     opts,
		viewport: viewport.New(defaultWidth, 1),
		input:    ta,
		ctx:      ctx,
		cancel:   cancel,
		now:      time.Now,
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
	case tea.KeyMsg:
		switch msg.String() {
		case "ctrl+c":
			return m.ctrlC()
		case "esc":
			if sess := m.opts.Session; sess != nil && sess.Busy() {
				sess.Cancel()
			}
			return m, nil
		case "enter":
			return m.submit()
		}
	}

	var cmds []tea.Cmd
	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)
	cmds = append(cmds, cmd)
	m.viewport, cmd = m.viewport.Update(msg)
	cmds = append(cmds, cmd)
	return m, tea.Batch(cmds...)
}

func (m Model) ctrlC() (tea.Model, tea.Cmd) {
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
		name, args, _ := strings.Cut(text, " ")
		if fn, ok := slashCommands[name]; ok {
			return fn(m, strings.TrimSpace(args))
		}
		m.add(blockInfo, "unknown command: "+name)
		return m, nil
	}
	m.add(blockPlain, "> "+text)
	sess := m.opts.Session
	if sess == nil {
		m.add(blockInfo, "no model configured")
		return m, nil
	}
	switch err := sess.Submit(text); {
	case err == nil:
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
		m.add(blockPlain, strings.TrimSpace("tool "+ev.Tool+" "+ev.Args))
	case agent.EvToolResult:
		m.add(blockPlain, ev.Text)
	case agent.EvApproval:
		if ev.Approval != nil {
			m.add(blockInfo, strings.TrimSpace("approval requested: "+ev.Approval.Tool+" "+ev.Approval.Args))
		}
	case agent.EvError:
		m.add(blockError, "error: "+ev.Text)
	case agent.EvInfo:
		m.add(blockInfo, ev.Text)
	case agent.EvSubagent:
		if ev.Text != "" {
			m.add(blockInfo, ev.Text)
		}
	case agent.EvTurnDone:
		m.streaming = false
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
func (m *Model) refresh() {
	parts := make([]string, len(m.blocks))
	for i, b := range m.blocks {
		text := m.redact(b.text)
		switch b.kind {
		case blockError:
			text = errorStyle.Render(text)
		case blockInfo:
			text = infoStyle.Render(text)
		}
		parts[i] = text
	}
	content := strings.Join(parts, "\n")
	m.viewport.SetContent(lipgloss.NewStyle().Width(m.viewport.Width).Render(content))
	m.viewport.GotoBottom()
}

func (m *Model) resize(w, h int) {
	if w < borderCols+1 {
		w = borderCols + 1
	}
	if h < 1 {
		h = 1
	}
	m.width, m.height = w, h
	m.input.SetWidth(w - borderCols)
	m.input.SetHeight(inputRows)
	vh := h - inputRows - borderRows - statusRows
	if vh < 1 {
		vh = 1
	}
	m.viewport.Width = w
	m.viewport.Height = vh
	m.refresh()
}

// View implements tea.Model.
func (m Model) View() string {
	box := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		Render(m.input.View())
	return lipgloss.JoinVertical(lipgloss.Left, m.viewport.View(), box, m.StatusLine())
}

// StatusLine returns the single status line shown under the input.
func (m Model) StatusLine() string {
	model, effort, mode := m.opts.ModelID, m.opts.Effort, m.opts.Mode
	tokens, ctxUsed := agent.Tokens{}, 0
	if sess := m.opts.Session; sess != nil {
		model, effort, mode = sess.ModelID(), sess.Effort(), string(sess.Mode())
		tokens = sess.Tokens()
		ctxUsed, _ = sess.ContextSize()
	}
	if model == "" {
		model = "no model"
	}
	return strings.Join([]string{
		m.opts.StartDir,
		model,
		effort,
		mode,
		fmt.Sprintf("in %d / out %d / total %d", tokens.In, tokens.Out, tokens.Total),
		fmt.Sprintf("ctx %d", ctxUsed),
	}, " · ")
}
