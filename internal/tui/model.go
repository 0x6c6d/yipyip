package tui

import (
	"strings"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// Options configures the initial state of the UI.
type Options struct {
	StartDir string
	ModelID  string // "" = no model
	Effort   string // "" = "medium"
	Mode     string // "" = "manual"
}

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
	lines    []string
	width    int
	height   int
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

	m := Model{
		opts:     opts,
		viewport: viewport.New(defaultWidth, 1),
		input:    ta,
	}
	m.resize(defaultWidth, defaultHeight)
	return m
}

// Init implements tea.Model.
func (m Model) Init() tea.Cmd {
	return textarea.Blink
}

// Update implements tea.Model. It always returns a tui.Model value.
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.resize(msg.Width, msg.Height)
		return m, nil
	case tea.KeyMsg:
		switch msg.String() {
		case "ctrl+c":
			return m, tea.Quit
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

func (m Model) submit() (tea.Model, tea.Cmd) {
	text := strings.TrimSpace(m.input.Value())
	if text == "" {
		return m, nil
	}
	if text == "/exit" || text == "/quit" {
		return m, tea.Quit
	}
	m.input.Reset()
	m.append("> "+text, "no model configured")
	return m, nil
}

func (m *Model) append(lines ...string) {
	m.lines = append(m.lines, lines...)
	m.viewport.SetContent(strings.Join(m.lines, "\n"))
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
	m.viewport.SetContent(strings.Join(m.lines, "\n"))
	m.viewport.GotoBottom()
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
	model := m.opts.ModelID
	if model == "" {
		model = "no model"
	}
	return strings.Join([]string{
		m.opts.StartDir,
		model,
		m.opts.Effort,
		m.opts.Mode,
		"in 0 / out 0 / total 0",
		"ctx 0",
	}, " · ")
}
