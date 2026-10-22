package agent

import (
	"context"
	"errors"
	"fmt"
	"time"

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
