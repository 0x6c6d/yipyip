package agent

import (
	"context"
	"errors"
	"fmt"
	"math"
	"time"

	"yipyip/internal/config"
	"yipyip/internal/provider"
)

// Errors of the jev toggle and of askJev.
var (
	ErrJevNotConfigured = errors.New("jev is not configured")
	ErrJevOff           = errors.New("jev is off")
)

// JevTimeout bounds one jev request; JevStateLimit bounds the state sent.
const (
	JevTimeout    = 10 * time.Second
	JevStateLimit = 2000 // runes of state sent per question
)

// JevStats are the in-memory jev counters of a session.
type JevStats struct {
	Answered, Fallbacks, InputTokens           int // jev decisions used / fallbacks / jev input tokens
	QuestionTokens, RoutingTokens, OtherTokens int // main-model tokens by category
}

// JevConfigured reports whether "jev" is configured with an apiKey.
func (s *Session) JevConfigured() bool { return s.JevClient() != nil }

// SetJev switches jev on or off. Switching it on without a configured jev
// fails with ErrJevNotConfigured and leaves it off.
func (s *Session) SetJev(on bool) error {
	if on && !s.JevConfigured() {
		return ErrJevNotConfigured
	}
	s.mu.Lock()
	s.jevOn = on
	s.mu.Unlock()
	return nil
}

// JevOn reports whether jev is on (off at start, never persisted).
func (s *Session) JevOn() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.jevOn
}

// JevStats returns a copy of the jev counters.
func (s *Session) JevStats() JevStats {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.jevStats
}

// SetJevTimeout sets the timeout of one jev request (test seam); d <= 0
// restores JevTimeout.
func (s *Session) SetJevTimeout(d time.Duration) {
	if d < 0 {
		d = 0
	}
	s.mu.Lock()
	s.jevTimeout = d
	s.mu.Unlock()
}

// askJev asks jev the questions qs about state. Without a configured jev or
// while jev is off it fails with ErrJevOff and sends nothing. The state is
// redacted first and then cut to JevStateLimit runes; the request is bounded
// by the jev timeout. Errors are returned as is so callers fall back.
func (s *Session) askJev(ctx context.Context, state string, qs []provider.JevQuestion) ([]provider.JevAnswer, error) {
	c := s.JevClient()
	s.mu.Lock()
	on, timeout := s.jevOn, s.jevTimeout
	s.mu.Unlock()
	if c == nil || !on {
		return nil, fmt.Errorf("agent: %w", ErrJevOff)
	}
	if timeout <= 0 {
		timeout = JevTimeout
	}
	state = truncateRunes(s.Redact(state), JevStateLimit)
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	ans, usage, err := c.Ask(ctx, state, qs)
	s.mu.Lock()
	s.jevStats.InputTokens += usage.Input
	s.mu.Unlock()
	return ans, err
}

// noteJev counts one jev decision: used when answered, else a fallback.
func (s *Session) noteJev(answered bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if answered {
		s.jevStats.Answered++
	} else {
		s.jevStats.Fallbacks++
	}
}

// difficultyQuestion is the jev score question that routes a turn.
var difficultyQuestion = provider.JevQuestion{
	Name:         "difficulty",
	Type:         provider.JevScore,
	Instructions: "Rate how hard the user's coding request is to carry out well.",
	Levels: []string{
		"easy: a question, a lookup or a small local change in one place",
		"medium: a change across a few files that needs some reading of the code",
		"hard: a design decision, a refactor or a bug that needs deep investigation",
	},
}

// Tier is the difficulty tier jev routed a turn to.
type Tier int

// The tiers of a turn; TierNone means the turn was not routed.
const (
	TierNone Tier = iota
	TierEasy
	TierMedium
	TierHard
)

// String returns the tier's name.
func (t Tier) String() string {
	switch t {
	case TierEasy:
		return "easy"
	case TierMedium:
		return "medium"
	case TierHard:
		return "hard"
	}
	return "not routed"
}

// scoreTier maps a difficulty score to a tier: rounded, then clamped to
// 0..2 (easy, medium, hard). A NaN or infinite score reports false.
func scoreTier(score float64) (Tier, bool) {
	if math.IsNaN(score) || math.IsInf(score, 0) {
		return TierNone, false
	}
	switch r := math.Round(score); {
	case r <= 0:
		return TierEasy, true
	case r == 1:
		return TierMedium, true
	default:
		return TierHard, true
	}
}

// tierModel returns the model id of tier t on route r and the effective
// tier: an absent easy or medium model uses hard, which is then effective.
func tierModel(r *config.JevRoute, t Tier) (string, Tier) {
	switch t {
	case TierEasy:
		if r.Easy != "" {
			return r.Easy, TierEasy
		}
	case TierMedium:
		if r.Medium != "" {
			return r.Medium, TierMedium
		}
	}
	return r.Hard, TierHard
}

// JevRoute returns the effective model id of each tier (an absent tier uses
// the hard id); all "" when jev or its route is not configured.
func (s *Session) JevRoute() (easy, medium, hard string) {
	if s.cfg.Jev == nil || s.cfg.Jev.Route == nil {
		return "", "", ""
	}
	r := s.cfg.Jev.Route
	easy, _ = tierModel(r, TierEasy)
	medium, _ = tierModel(r, TierMedium)
	return easy, medium, r.Hard
}

// routeTurn picks the model of a main-agent turn with jev when jev is on and
// "jev.route" is configured: the difficulty score is rounded and clamped to
// 0..2 (easy, medium, hard) and the tier's model runs the whole turn (an
// absent easy or medium model uses hard). It returns "" (the session's
// model) when not routed: jev off, no route, or a failed, timed out,
// malformed or unconfident answer (counted as a fallback). It never changes
// the session's model.
func (s *Session) routeTurn(ctx context.Context, user string) string {
	if !s.JevOn() || s.cfg.Jev == nil || s.cfg.Jev.Route == nil {
		return ""
	}
	ans, err := s.askJev(ctx, "request: "+user, []provider.JevQuestion{difficultyQuestion})
	if ctx.Err() != nil {
		return ""
	}
	if err != nil || len(ans) != 1 || ans[0].Confidence < s.cfg.Jev.MinConfidence {
		s.noteJev(false)
		return ""
	}
	tier, ok := scoreTier(ans[0].Score)
	if !ok {
		s.noteJev(false)
		return ""
	}
	id, tier := tierModel(s.cfg.Jev.Route, tier)
	s.noteJev(true)
	s.mu.Lock()
	s.turnRouted, s.turnModel, s.turnTier = true, id, tier
	s.mu.Unlock()
	s.emit(Event{Kind: EvInfo, AgentID: MainAgentID, Text: "jev route: " + id})
	return id
}

// riskQuestion is the jev noul question of the auto-mode risk screen.
var riskQuestion = provider.JevQuestion{
	Name:         "risk",
	Type:         provider.JevNoul,
	Instructions: "Is this tool call risky: destructive, irreversible, leaking secrets or acting outside the project?",
}

// jevRisk is the session's risk screen (see Gate.SetRiskScreen): it asks jev
// whether an auto-mode call of tool with args is risky and reports true for a
// probability >= 0.5, which only routes the call into the approval prompt.
// It is false at once while jev is off and on a failed or timed out answer
// (counted as a fallback). When ctx is done it reports true without counting,
// so the call meets the prompt, which fails closed. It never approves a call
// and never touches mode, "always" or the gate.
func (s *Session) jevRisk(ctx context.Context, tool, args string) bool {
	if !s.JevOn() {
		return false
	}
	ans, err := s.askJev(ctx, "tool: "+tool+"\nargs: "+args, []provider.JevQuestion{riskQuestion})
	if ctx.Err() != nil {
		return true
	}
	if err != nil || len(ans) != 1 {
		s.noteJev(false)
		return false
	}
	s.noteJev(true)
	if ans[0].Noul < 0.5 {
		return false
	}
	s.emit(Event{Kind: EvInfo, AgentID: MainAgentID, Text: "jev: risky " + tool + " call, asking for approval"})
	return true
}

// TurnModel returns the model id jev routed the running or last turn to, ""
// when that turn was not routed.
func (s *Session) TurnModel() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.turnModel
}

// TurnTier returns the effective tier of the running or last turn, TierNone
// when that turn was not routed.
func (s *Session) TurnTier() Tier {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.turnTier
}

// truncateRunes keeps at most n runes of the start of text.
func truncateRunes(text string, n int) string {
	i := 0
	for pos := range text {
		if i == n {
			return text[:pos]
		}
		i++
	}
	return text
}

// accountJev adds the total of one main-model response to its category:
// question when the reply calls the question tool, else routing while the
// turn is routed, else other.
// The caller holds s.mu.
func (s *Session) accountJev(calls []provider.ToolCall, total int) {
	for _, tc := range calls {
		if tc.Name == toolQuestion {
			s.jevStats.QuestionTokens += total
			return
		}
	}
	if s.turnRouted {
		s.jevStats.RoutingTokens += total
	} else {
		s.jevStats.OtherTokens += total
	}
}
