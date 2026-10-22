package agent

import (
	"context"
	"encoding/json"
	"slices"
	"strings"
	"sync"
	"unicode/utf8"

	"yipyip/internal/provider"
	"yipyip/internal/tools"
)

// Cancelled is the answer of a question the user dismissed (Esc) or that was
// cancelled with its turn. It reaches the model as the tool result.
const Cancelled = "cancelled"

// MaxQuestionOptions bounds the option labels of one question; more are
// dropped silently.
const MaxQuestionOptions = 6

// toolQuestion is the name of the question tool. It exists only in the agent
// wiring (never in the tools registry) and is offered to the main agent only:
// a subagent has no channel to the user.
const toolQuestion = "question"

const questionSchema = `{"type":"object","properties":{"question":{"type":"string","description":"The question to ask the user."},"options":{"type":"array","items":{"type":"string"},"description":"Up to 6 answer options; the user may also answer with free text."},"multiple":{"type":"boolean","description":"Whether the user may choose several options (default false)."}},"required":["question"]}`

var questionDef = provider.ToolDef{
	Name:        toolQuestion,
	Description: "Ask the user a question and wait for the answer. Returns the chosen option label(s) joined by \", \", the user's free text, or \"cancelled\".",
	Parameters:  json.RawMessage(questionSchema),
}

// QuestionRequest is one pending question of the question tool to the user.
type QuestionRequest struct {
	AgentID  string
	Question string   // the model's question text (redacted)
	Options  []string // up to MaxQuestionOptions option labels (redacted)
	Multiple bool

	reply chan string // buffered 1
	once  sync.Once
}

// Respond answers the request. It never blocks; the first call (Respond or
// Cancel) wins and later calls are ignored. Responding to a request not
// created by the question tool is a no-op.
func (r *QuestionRequest) Respond(answer string) {
	if r == nil || r.reply == nil {
		return
	}
	r.once.Do(func() {
		select {
		case r.reply <- answer:
		default:
		}
	})
}

// Cancel answers the request with Cancelled. It is idempotent.
func (r *QuestionRequest) Cancel() {
	r.Respond(Cancelled)
}

// JoinAnswers joins the labels chosen in a multiple-choice question into the
// answer string ("x, y").
func JoinAnswers(labels []string) string {
	return strings.Join(labels, ", ")
}

// newQuestionRequest parses the question tool's arguments into a request.
func newQuestionRequest(agentID, args string) (*QuestionRequest, error) {
	var a struct {
		Question string   `json:"question"`
		Options  []string `json:"options"`
		Multiple bool     `json:"multiple"`
	}
	if err := decodeArgs(args, &a); err != nil {
		return nil, err
	}
	if len(a.Options) > MaxQuestionOptions {
		a.Options = a.Options[:MaxQuestionOptions]
	}
	return &QuestionRequest{
		AgentID:  agentID,
		Question: a.Question,
		Options:  append([]string(nil), a.Options...),
		Multiple: a.Multiple,
		reply:    make(chan string, 1),
	}, nil
}

// questionTool runs the question tool for agentID: it reports the request to
// the UI as EvQuestion and blocks until it is answered or ctx is done. On ctx
// done the request is cancelled (a late answer is ignored) and Cancelled is
// returned; the loop then unwinds the turn like a cancelled approval.
func (s *Session) questionTool(ctx context.Context, agentID, args string) tools.Result {
	req, err := newQuestionRequest(agentID, args)
	if err != nil {
		return errorResult("invalid arguments: %v", err)
	}
	req.Question = s.Redact(req.Question)
	for i, o := range req.Options {
		req.Options[i] = s.Redact(o)
	}
	if ctx.Err() != nil {
		req.Cancel()
		return tools.Result{Output: Cancelled}
	}
	if label, ok := s.jevAnswer(ctx, req); ok {
		req.Cancel()
		return tools.Result{Output: label}
	}
	if ctx.Err() != nil {
		req.Cancel()
		return tools.Result{Output: Cancelled}
	}
	s.emit(Event{Kind: EvQuestion, AgentID: agentID, Tool: toolQuestion, Text: req.Question, Question: req})
	select {
	case answer := <-req.reply:
		return tools.Result{Output: s.Redact(answer)}
	case <-ctx.Done():
		req.Cancel() // close the request so a late answer is ignored
		return tools.Result{Output: Cancelled}
	}
}

// jevAnswer lets jev answer req on the user's behalf. Jev is asked only while
// it is on and only for a single-choice question with at least 2 options;
// its choice is used only when it is one of the options and its confidence
// reaches the configured minimum. Every other outcome (error, timeout, low
// confidence, unknown choice) is counted as a fallback and reports false, so
// the user is asked as usual. A cancelled turn is not counted.
func (s *Session) jevAnswer(ctx context.Context, req *QuestionRequest) (string, bool) {
	if !s.JevOn() || req.Multiple || len(req.Options) < 2 || s.cfg.Jev == nil {
		return "", false
	}
	choices := make(map[string]string, len(req.Options))
	for _, o := range req.Options {
		choices[o] = o
	}
	q := provider.JevQuestion{
		Name:         "answer",
		Type:         provider.JevChoice,
		Instructions: "Pick the option the user would most likely choose.",
		Choices:      choices,
	}
	ans, err := s.askJev(ctx, jevQuestionState(req, turnHistory(ctx)), []provider.JevQuestion{q})
	if ctx.Err() != nil {
		return "", false
	}
	if err == nil && len(ans) == 1 && ans[0].Confidence >= s.cfg.Jev.MinConfidence && slices.Contains(req.Options, ans[0].Choice) {
		s.noteJev(true)
		s.emit(Event{Kind: EvInfo, AgentID: req.AgentID, Text: "jev answered: " + req.Question + " → " + ans[0].Choice})
		return ans[0].Choice, true
	}
	s.noteJev(false)
	return "", false
}

// jevQuestionState builds the jev state of a question: the question, the
// option labels and the tail of the user/assistant messages of hist, cut so
// that the whole state stays within JevStateLimit runes.
func jevQuestionState(req *QuestionRequest, hist []provider.Message) string {
	head := "question: " + req.Question + "\noptions: " + strings.Join(req.Options, ", ") + "\nconversation:\n"
	var conv strings.Builder
	for _, m := range hist {
		if (m.Role == "user" || m.Role == "assistant") && m.Content != "" {
			conv.WriteString(m.Role + ": " + m.Content + "\n")
		}
	}
	budget := max(JevStateLimit-utf8.RuneCountInString(head), 0)
	tail := []rune(conv.String())
	if len(tail) > budget {
		tail = tail[len(tail)-budget:]
	}
	return head + string(tail)
}

type turnHistoryKey struct{}

// withTurnHistory attaches the loop's current history to ctx for the
// question tool (the jev state).
func withTurnHistory(ctx context.Context, hist []provider.Message) context.Context {
	return context.WithValue(ctx, turnHistoryKey{}, hist)
}

// turnHistory returns the history attached by withTurnHistory, or nil.
func turnHistory(ctx context.Context) []provider.Message {
	h, _ := ctx.Value(turnHistoryKey{}).([]provider.Message)
	return h
}
