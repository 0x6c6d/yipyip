package agent

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"yipyip/internal/provider"
)

// ErrNothingToCompact is returned by Compact when the history has at most two
// turns.
var ErrNothingToCompact = errors.New("nothing to compact")

// keptTurns is the number of most recent turns that compaction keeps verbatim.
const keptTurns = 2

// autoCompactPercent is the share of the context limit at which a turn first
// compacts the history.
const autoCompactPercent = 80

const (
	summaryRequest = "Summarize the conversation so far concisely: the decisions made, the files touched and the open tasks. Reply with the summary only."
	summaryPrefix  = "[Summary of the earlier conversation]\n"
)

// Compact replaces the older part of the conversation by a model-written
// summary and keeps the last two turns verbatim (a turn is a user message and
// everything up to the next user message). It is synchronous. It returns
// ErrBusy while a turn runs or another compaction is in progress and
// ErrNothingToCompact when the history has at most two turns; on any other
// error the history is unchanged.
func (s *Session) Compact(ctx context.Context) error {
	_, _, clientErr := s.newClient()
	s.mu.Lock()
	if s.busy {
		s.mu.Unlock()
		return ErrBusy
	}
	hist := copyHistory(s.history)
	if _, ok := compactCut(hist); !ok {
		s.mu.Unlock()
		return ErrNothingToCompact
	}
	if clientErr != nil {
		s.mu.Unlock()
		return clientErr
	}
	ctx, cancel := context.WithCancel(ctx)
	done := make(chan struct{})
	s.busy = true
	s.cancelTurn = cancel
	s.turnDone = done
	gen := s.gen
	s.mu.Unlock()
	defer func() {
		cancel()
		s.mu.Lock()
		s.busy = false
		s.cancelTurn = nil
		s.turnDone = nil
		s.mu.Unlock()
		close(done)
	}()

	newHist, cut, err := s.summarize(ctx, gen, hist)
	if err != nil {
		return err
	}
	if !s.applyCompaction(gen, len(hist), cut, newHist) {
		return nil // the session was reset meanwhile: the result is stale
	}
	s.emit(Event{Kind: EvInfo, AgentID: MainAgentID, Text: fmt.Sprintf("compacted: %d messages -> %d", len(hist), len(newHist))})
	return nil
}

// autoCompact runs at the start of a turn (the turn owns the busy flag): when
// auto-compaction is enabled and the last prompt filled at least 80% of the
// known context limit, it compacts the history as it was before the turn's
// user message (the last element of hist) and returns the new history with the
// user message re-appended. Failures are reported and the turn continues with
// hist unchanged.
func (s *Session) autoCompact(ctx context.Context, gen uint64, hist []provider.Message) []provider.Message {
	used, limit := s.ContextSize()
	if !s.cfg.AutoCompact || limit <= 0 || used*100 < limit*autoCompactPercent {
		return hist
	}
	prefix, user := hist[:len(hist)-1], hist[len(hist)-1]
	newPrefix, cut, err := s.summarize(ctx, gen, prefix)
	if err != nil {
		if !errors.Is(err, ErrNothingToCompact) && ctx.Err() == nil {
			s.emit(Event{Kind: EvError, AgentID: MainAgentID, Text: "auto-compact failed: " + err.Error()})
		}
		return hist
	}
	if !s.applyCompaction(gen, len(prefix), cut, newPrefix) {
		return hist
	}
	s.emit(Event{Kind: EvInfo, AgentID: MainAgentID, Text: fmt.Sprintf("auto-compacted: %d messages -> %d", len(prefix), len(newPrefix))})
	return append(newPrefix[:len(newPrefix):len(newPrefix)], user)
}

// compactCut returns the index of the first message of the oldest kept turn.
// It reports false when the history has at most keptTurns turns.
func compactCut(hist []provider.Message) (int, bool) {
	var users []int
	for i, m := range hist {
		if m.Role == "user" {
			users = append(users, i)
		}
	}
	if len(users) <= keptTurns {
		return 0, false
	}
	return users[len(users)-keptTurns], true
}

// summarize asks the model (without tools) for a summary of hist and returns
// the compacted history together with the index in hist where the kept turns
// start. It does not modify the session except for the token totals.
func (s *Session) summarize(ctx context.Context, gen uint64, hist []provider.Message) ([]provider.Message, int, error) {
	cut, ok := compactCut(hist)
	if !ok {
		return nil, 0, ErrNothingToCompact
	}
	client, m, err := s.newClient()
	if err != nil {
		return nil, 0, err
	}
	msgs := append(copyHistory(hist), provider.Message{Role: "user", Content: summaryRequest})
	req := provider.Request{
		Model:      m.Key,
		Messages:   msgs,
		Effort:     s.Effort(),
		SendEffort: m.Reasoning,
	}
	text, calls, usage, err := collectReply(ctx, client, req, nil)
	if err != nil {
		if cerr := ctx.Err(); cerr != nil {
			return nil, 0, cerr
		}
		return nil, 0, err
	}
	// The agent id keeps this request from setting the context size.
	s.account(gen, "compact", req.Messages, text, calls, usage)
	summary := strings.TrimSpace(s.Redact(text))
	if summary == "" {
		return nil, 0, errors.New("compaction failed: empty summary")
	}
	out := make([]provider.Message, 0, 2+len(hist)-cut)
	out = append(out, hist[0], provider.Message{Role: "user", Content: summaryPrefix + summary})
	out = append(out, copyHistory(hist[cut:])...)
	return out, cut, nil
}

// applyCompaction installs the compacted history of generation gen. The first
// oldLen messages of the current history are replaced by newPrefix (whose kept
// turns start at index 2 and came from index cut); messages after oldLen (the
// user message of a turn that is starting) are kept. The undo journal drops
// the records of the dropped turns and re-indexes the others. It reports false
// when the session was reset meanwhile and nothing was changed.
func (s *Session) applyCompaction(gen uint64, oldLen, cut int, newPrefix []provider.Message) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.gen != gen || oldLen > len(s.history) {
		return false
	}
	tail := s.history[oldLen:]
	newHist := make([]provider.Message, 0, len(newPrefix)+len(tail))
	newHist = append(newHist, newPrefix...)
	newHist = append(newHist, tail...)
	s.history = newHist
	kept := s.journal[:0:0]
	for _, rec := range s.journal {
		switch {
		case rec.histLen >= oldLen: // the turn that is starting
			rec.histLen += len(newPrefix) - oldLen
		case rec.histLen >= cut:
			rec.histLen += 2 - cut
		default: // a dropped turn
			continue
		}
		kept = append(kept, rec)
	}
	s.journal = kept
	s.lastPromptTokens = estimateTokens(messageChars(newHist))
	return true
}
