package agent

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"sync"

	"yipyip/internal/config"
	"yipyip/internal/provider"
	"yipyip/internal/tools"
)

// Session errors.
var (
	ErrNoModel       = errors.New("no model configured")
	ErrBusy          = errors.New("a turn is already running")
	ErrUnknownModel  = errors.New("unknown model")
	ErrInvalidEffort = errors.New("invalid effort")
)

// Effort levels.
const (
	EffortLow    = "low"
	EffortMedium = "medium"
	EffortHigh   = "high"
)

// Options configure a new Session.
type Options struct {
	Config     *config.Config
	StartDir   string
	HTTPClient *http.Client // nil -> provider.NewHTTPClient(Config.BaseURLs())
}

// Tokens are the accumulated token counts of a session.
type Tokens struct{ In, Out, Total int }

// Session is one conversation of the main agent with its tools, approval gate
// and event queue. All methods are safe for concurrent use.
type Session struct {
	cfg      *config.Config
	startDir string // absolute, symlink-resolved
	hc       *http.Client
	registry *tools.Registry
	gate     *Gate
	events   *eventQueue

	mu               sync.Mutex
	modelID          string
	effort           string
	mode             Mode
	sessID           string // stable per-conversation id, sent as x-opencode-session
	systemPrompt     string
	history          []provider.Message // [0] is the system prompt
	tokens           Tokens
	lastPromptTokens int
	busy             bool
	cancelTurn       func()        // cancels the running turn; nil when idle
	turnDone         chan struct{} // closed when the running turn ends; nil when idle
	gen              uint64        // incremented by Reset so a stale turn can detect it
	subs             *Manager      // subagents of the current generation
	journal          []*turnRecord // undo journal, one record per turn (memory only)

	jmu sync.Mutex // guards the snapshots and bash lists of the journal records
}

// NewSession returns a session for opts.StartDir with the defaults: the
// config's default model, effort medium, manual mode, zero tokens and a
// history holding only the system prompt.
func NewSession(opts Options) (*Session, error) {
	if opts.Config == nil {
		return nil, errors.New("agent: nil config")
	}
	cfg := opts.Config
	hc := opts.HTTPClient
	if hc == nil {
		var err error
		hc, err = provider.NewHTTPClient(cfg.BaseURLs())
		if err != nil {
			// A base URL may contain an {env:} value and URL parse errors
			// quote the URL.
			return nil, &redactedError{msg: "agent: " + cfg.Redactor.Redact(err.Error()), err: err}
		}
	}
	s := &Session{
		cfg:     cfg,
		hc:      hc,
		events:  newEventQueue(),
		modelID: cfg.DefaultModelID(),
		effort:  EffortMedium,
		mode:    ModeManual,
		sessID:  newSessionID(),
	}
	reg, err := tools.NewRegistry(tools.Options{
		StartDir:    opts.StartDir,
		Redactor:    cfg.Redactor,
		StripEnv:    cfg.EnvNames,
		BeforeWrite: func(path string) { s.beforeWrite(path) },
	})
	if err != nil {
		return nil, &redactedError{msg: "agent: " + cfg.Redactor.Redact(err.Error()), err: err}
	}
	s.registry = reg
	s.startDir = reg.Guard().Root()
	s.gate = NewGate(func(r *ApprovalRequest) {
		s.emit(Event{Kind: EvApproval, AgentID: r.AgentID, Tool: r.Tool, Args: r.Args, Approval: r})
	})
	s.systemPrompt = buildSystemPrompt(s.startDir, reg.Guard(), cfg.Redactor)
	s.history = []provider.Message{{Role: "system", Content: s.systemPrompt}}
	s.subs = s.newManager(0)
	return s, nil
}

// ModelID returns the current model id ("" when none is configured).
func (s *Session) ModelID() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.modelID
}

// Models returns all configured model ids in a stable (sorted) order.
func (s *Session) Models() []string {
	return s.cfg.ModelIDs()
}

// SetModel switches to model id. An unknown id yields an error wrapping
// ErrUnknownModel and leaves the model unchanged.
func (s *Session) SetModel(id string) error {
	if _, _, ok := s.cfg.Lookup(id); !ok {
		return fmt.Errorf("%w: %q", ErrUnknownModel, s.Redact(id))
	}
	s.mu.Lock()
	s.modelID = id
	s.mu.Unlock()
	return nil
}

// Effort returns the reasoning effort (low, medium or high).
func (s *Session) Effort() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.effort
}

// SetEffort sets the reasoning effort. Anything other than low, medium or
// high yields an error wrapping ErrInvalidEffort and leaves it unchanged.
func (s *Session) SetEffort(e string) error {
	switch e {
	case EffortLow, EffortMedium, EffortHigh:
	default:
		return fmt.Errorf("%w: %q (want low, medium or high)", ErrInvalidEffort, s.Redact(e))
	}
	s.mu.Lock()
	s.effort = e
	s.mu.Unlock()
	return nil
}

// ReasoningEnabled reports whether the current model supports reasoning
// effort (false when no model is configured).
func (s *Session) ReasoningEnabled() bool {
	_, m, ok := s.currentModel()
	return ok && m.Reasoning
}

// currentModel returns the provider and model of the current model id.
func (s *Session) currentModel() (config.Provider, config.Model, bool) {
	return s.cfg.Lookup(s.ModelID())
}

// newClient returns a provider client for the current model, or an error
// wrapping ErrNoModel when none is configured.
func (s *Session) newClient() (*provider.Client, config.Model, error) {
	return s.clientFor("")
}

// clientFor returns a provider client for model id ("" = the current model),
// or an error wrapping ErrNoModel when it is not configured.
func (s *Session) clientFor(id string) (*provider.Client, config.Model, error) {
	if id == "" {
		id = s.ModelID()
	}
	p, m, ok := s.cfg.Lookup(id)
	if !ok {
		return nil, config.Model{}, ErrNoModel
	}
	c := provider.NewClient(p.BaseURL, p.APIKey, s.hc, s.cfg.Redactor)
	s.mu.Lock()
	c.SetSession(s.sessID)
	s.mu.Unlock()
	return c, m, nil
}

// newSessionID returns a stable conversation id: "ses_" plus 12 random bytes
// in hex, as expected by gateways that route requests by session.
func newSessionID() string {
	var b [12]byte
	if _, err := rand.Read(b[:]); err != nil {
		// crypto/rand never fails in practice; fall back to a zero id rather
		// than losing requests.
		return ""
	}
	return "ses_" + hex.EncodeToString(b[:])
}

// Mode returns the approval mode.
func (s *Session) Mode() Mode {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.mode
}

// SetMode sets the approval mode.
func (s *Session) SetMode(m Mode) {
	s.mu.Lock()
	s.mode = m
	s.mu.Unlock()
}

// ToggleMode switches between manual and auto mode and returns the new mode.
func (s *Session) ToggleMode() Mode {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.mode == ModeAuto {
		s.mode = ModeManual
	} else {
		s.mode = ModeAuto
	}
	return s.mode
}

// Tokens returns the accumulated token counts.
func (s *Session) Tokens() Tokens {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.tokens
}

// ContextSize returns the prompt tokens of the last request and the current
// model's context limit (0 = unknown).
func (s *Session) ContextSize() (used, limit int) {
	s.mu.Lock()
	used = s.lastPromptTokens
	s.mu.Unlock()
	if _, m, ok := s.currentModel(); ok {
		limit = m.ContextLimit
	}
	return used, limit
}

// History returns a copy of the conversation; [0] is the system prompt.
func (s *Session) History() []provider.Message {
	s.mu.Lock()
	defer s.mu.Unlock()
	return copyHistory(s.history)
}

func copyHistory(h []provider.Message) []provider.Message {
	out := make([]provider.Message, len(h))
	for i, m := range h {
		out[i] = m
		if m.ToolCalls != nil {
			out[i].ToolCalls = append([]provider.ToolCall(nil), m.ToolCalls...)
		}
	}
	return out
}

// SystemPrompt returns the current system prompt.
func (s *Session) SystemPrompt() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.systemPrompt
}

// StartDir returns the absolute, symlink-resolved start directory.
func (s *Session) StartDir() string { return s.startDir }

// Redact masks every configured secret in text.
func (s *Session) Redact(text string) string {
	return s.cfg.Redactor.Redact(text)
}

// Reset starts a new conversation (/new): it cancels a running turn and all
// subagents, starts a new empty subagent manager (ids restart at a1), re-reads
// the system prompt, clears the history and token counts, forgets "always"
// approvals, clears the undo journal and drops queued events. Model, effort
// and mode are kept, and a fresh session id is generated.
func (s *Session) Reset() {
	prompt := buildSystemPrompt(s.startDir, s.registry.Guard(), s.cfg.Redactor)
	s.mu.Lock()
	cancel := s.cancelTurn
	done := s.turnDone
	oldSubs := s.subs
	s.cancelTurn = nil
	s.gen++
	s.subs = s.newManager(s.gen)
	s.systemPrompt = prompt
	s.history = []provider.Message{{Role: "system", Content: prompt}}
	s.tokens = Tokens{}
	s.lastPromptTokens = 0
	s.journal = nil
	s.sessID = newSessionID()
	s.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	oldSubs.CancelAll()
	// Wait for the cancelled turn without holding the mutex; its stale
	// generation keeps it from writing into the new history.
	if done != nil {
		<-done
	}
	s.gate.Reset()
	s.events.drain()
}

// redactedError is an error whose text has been redacted; it still wraps the
// original error for errors.Is/As.
type redactedError struct {
	msg string
	err error
}

func (e *redactedError) Error() string { return e.msg }
func (e *redactedError) Unwrap() error { return e.err }
