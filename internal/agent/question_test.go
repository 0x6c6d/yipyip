package agent

import (
	"context"
	"encoding/json"
	"slices"
	"strings"
	"testing"
	"time"

	"yipyip/internal/provider/providertest"
)

// questionServer scripts a main agent that asks one question with args and,
// once the question has a result, answers "follow-up".
func questionServer(args string) *providertest.Server {
	return providertest.NewFunc(func(_ int, req providertest.Request) providertest.Response {
		if countTools(req) == 0 {
			return fakeResp{ToolCalls: []fakeCall{{ID: "q1", Name: "question", Arguments: args}}}
		}
		return fakeResp{Text: "follow-up"}
	})
}

// runQuestionTurn submits a turn and calls answer for every EvQuestion; it
// returns all events of the turn (ending with EvTurnDone).
func runQuestionTurn(t *testing.T, s *Session, answer func(*QuestionRequest)) []Event {
	t.Helper()
	if err := s.Submit("ask me"); err != nil {
		t.Fatalf("Submit: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	var evs []Event
	for {
		ev, err := s.NextEvent(ctx)
		if err != nil {
			t.Fatalf("NextEvent: %v (events %+v)", err, evs)
		}
		evs = append(evs, ev)
		if ev.Kind == EvQuestion {
			if ev.Question == nil {
				t.Fatalf("EvQuestion without request: %+v", ev)
			}
			answer(ev.Question)
		}
		if ev.Kind == EvTurnDone {
			break
		}
	}
	waitTurn(t, s, 5*time.Second)
	return evs
}

// questionResults returns the tool results recorded in history for call q1.
func questionResults(s *Session) []string {
	var out []string
	for _, m := range s.History() {
		if m.Role == "tool" && m.ToolCallID == "q1" {
			out = append(out, m.Content)
		}
	}
	return out
}

// assertContinued checks that the turn ended normally with the follow-up.
func assertContinued(t *testing.T, s *Session, evs []Event) {
	t.Helper()
	for _, ev := range evs {
		if ev.Kind == EvError {
			t.Errorf("error event: %+v", ev)
		}
		if ev.Kind == EvInfo && ev.Text == CancelledMessage {
			t.Errorf("turn reported as cancelled: %+v", ev)
		}
	}
	h := s.History()
	if last := h[len(h)-1]; last.Role != "assistant" || last.Content != "follow-up" {
		t.Errorf("no follow-up reply, last entry %+v", last)
	}
}

func TestQuestionToolOfferedAndUngated(t *testing.T) {
	srv := providertest.New(fakeResp{Text: "hi"})
	defer srv.Close()
	s, _ := newLoopSession(t, srv)
	submitAndWait(t, s, "hello")

	reqs := srv.Requests()
	if len(reqs) != 1 || !slices.Contains(reqs[0].ToolNames, "question") {
		t.Fatalf("main request does not offer question: %+v", reqs)
	}
	if !s.turnTools(s.currentSubs(), &spawnedSubs{}).offers("question") {
		t.Error("main tool set does not offer question")
	}
	if !strings.Contains(s.SystemPrompt(), "question tool") {
		t.Error("system prompt does not mention the question tool")
	}
	if IsGated("question") {
		t.Error(`IsGated("question") = true`)
	}
	// The tool exists only in the agent wiring, never in the registry.
	if s.registry.Has("question") {
		t.Error("registry has a question tool")
	}
	res := s.registry.Run(context.Background(), "question", json.RawMessage(`{"question":"q"}`))
	if !res.IsError || !strings.Contains(res.Output, "unknown tool") {
		t.Errorf("registry lookup of question = %+v, want an unknown-tool error", res)
	}
	// A subagent is not offered the question tool.
	if s.subTools(&Subagent{}).offers("question") {
		t.Error("subagent tool set offers question")
	}
}

func TestQuestionSingleChoiceResult(t *testing.T) {
	srv := questionServer(`{"question":"pick one","options":["a","b","c"],"multiple":false}`)
	defer srv.Close()
	s, _ := newLoopSession(t, srv) // manual mode: the question is never gated
	var got *QuestionRequest
	evs := runQuestionTurn(t, s, func(r *QuestionRequest) {
		got = r
		r.Respond("b")
	})
	if got.AgentID != MainAgentID || got.Question != "pick one" || got.Multiple ||
		!slices.Equal(got.Options, []string{"a", "b", "c"}) {
		t.Errorf("request = %+v", got)
	}
	for _, ev := range evs {
		if ev.Kind == EvApproval {
			t.Errorf("question asked for approval: %+v", ev)
		}
	}
	if r := questionResults(s); !slices.Equal(r, []string{"b"}) {
		t.Errorf("history results = %q, want [b]", r)
	}
	if rs := toolResults(evs, "question"); len(rs) != 1 || rs[0].Text != "b" || rs[0].IsError {
		t.Errorf("result events = %+v", rs)
	}
	// The model got the answer as the tool result.
	reqs := srv.Requests()
	if len(reqs) != 2 {
		t.Fatalf("requests = %d", len(reqs))
	}
	m := reqs[1].Messages
	if last := m[len(m)-1]; last.Role != "tool" || last.ToolCallID != "q1" || last.Content != "b" {
		t.Errorf("model got %+v", last)
	}
	assertContinued(t, s, evs)
}

func TestQuestionMultipleResultJoined(t *testing.T) {
	if got := JoinAnswers([]string{"x", "y"}); got != "x, y" {
		t.Fatalf("JoinAnswers = %q", got)
	}
	srv := questionServer(`{"question":"pick some","options":["x","y","z"],"multiple":true}`)
	defer srv.Close()
	s, _ := newLoopSession(t, srv)
	evs := runQuestionTurn(t, s, func(r *QuestionRequest) {
		if !r.Multiple {
			t.Errorf("request not multiple: %+v", r)
		}
		r.Respond(JoinAnswers([]string{"x", "y"}))
	})
	if r := questionResults(s); !slices.Equal(r, []string{"x, y"}) {
		t.Errorf("history results = %q, want [x, y]", r)
	}
	assertContinued(t, s, evs)

	// Free text is returned verbatim.
	srv2 := questionServer(`{"question":"name?"}`)
	defer srv2.Close()
	s2, _ := newLoopSession(t, srv2)
	const free = "  call it Foo, please  "
	evs = runQuestionTurn(t, s2, func(r *QuestionRequest) {
		if r.Multiple || len(r.Options) != 0 {
			t.Errorf("defaults wrong: %+v", r)
		}
		r.Respond(free)
	})
	if r := questionResults(s2); !slices.Equal(r, []string{free}) {
		t.Errorf("free-text results = %q", r)
	}
	assertContinued(t, s2, evs)
}

func TestQuestionEscCancelled(t *testing.T) {
	if Cancelled != "cancelled" {
		t.Fatalf("Cancelled = %q", Cancelled)
	}
	srv := questionServer(`{"question":"go on?","options":["yes","no"]}`)
	defer srv.Close()
	s, _ := newLoopSession(t, srv)
	evs := runQuestionTurn(t, s, func(r *QuestionRequest) { r.Respond(Cancelled) })
	if r := questionResults(s); !slices.Equal(r, []string{"cancelled"}) {
		t.Errorf("history results = %q, want [cancelled]", r)
	}
	if rs := toolResults(evs, "question"); len(rs) != 1 || rs[0].IsError {
		t.Errorf("cancelled result must not be an error: %+v", rs)
	}
	assertContinued(t, s, evs)

	// Cancel() on the request is the same answer and idempotent.
	srv2 := questionServer(`{"question":"again?"}`)
	defer srv2.Close()
	s2, _ := newLoopSession(t, srv2)
	evs = runQuestionTurn(t, s2, func(r *QuestionRequest) {
		r.Cancel()
		r.Cancel()
		r.Respond("late")
	})
	if r := questionResults(s2); !slices.Equal(r, []string{Cancelled}) {
		t.Errorf("history results after Cancel = %q", r)
	}
	assertContinued(t, s2, evs)
	// Nil and foreign requests are harmless.
	var nr *QuestionRequest
	nr.Respond("x")
	nr.Cancel()
	(&QuestionRequest{}).Respond("x")
}

// untilKind collects the events of a submitted turn until the wait event
// (want) arrives and returns it with the events seen so far.
func untilKind(t *testing.T, s *Session, want EventKind) (Event, []Event) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	var evs []Event
	for {
		ev, err := s.NextEvent(ctx)
		if err != nil {
			t.Fatalf("NextEvent: %v (events %+v)", err, evs)
		}
		evs = append(evs, ev)
		if ev.Kind == want {
			return ev, evs
		}
		if ev.Kind == EvTurnDone {
			t.Fatalf("turn finished before event %v: %+v", want, evs)
		}
	}
}

// cancelAndCollect cancels the turn of s and returns the events emitted
// afterwards (up to and including EvTurnDone).
func cancelAndCollect(t *testing.T, s *Session) []Event {
	t.Helper()
	s.Cancel()
	waitTurn(t, s, 5*time.Second)
	evs := s.PollEvents()
	if len(evs) == 0 || evs[len(evs)-1].Kind != EvTurnDone {
		t.Fatalf("cancelled turn did not end with EvTurnDone: %+v", evs)
	}
	return evs
}

func TestQuestionNeverFinishesTurnWhileWaiting(t *testing.T) {
	srv := questionServer(`{"question":"wait?","options":["a"]}`)
	defer srv.Close()
	s, _ := newLoopSession(t, srv)
	s.SetMode(ModeAuto) // the question runs (and waits) in auto mode as well
	if err := s.Submit("ask me"); err != nil {
		t.Fatal(err)
	}
	ev, _ := untilKind(t, s, EvQuestion)
	req := ev.Question

	// Nobody answers: the turn holds.
	if !s.Busy() {
		t.Fatal("not busy while the question waits")
	}
	waited := make(chan struct{})
	go func() { s.Wait(); close(waited) }()
	select {
	case <-waited:
		t.Fatal("turn finished while the question waits")
	default:
	}
	for _, e := range s.PollEvents() {
		if e.Kind == EvTurnDone || e.Kind == EvToolResult {
			t.Fatalf("event %+v while the question waits", e)
		}
	}
	if active, _, _ := s.TurnState(); !active {
		t.Error("TurnState not active while the question waits")
	}

	// Cancelling the turn unwinds like a cancelled approval.
	qEvs := cancelAndCollect(t, s)
	<-waited
	req.Respond("late") // a late answer is ignored and never blocks
	if r := questionResults(s); !slices.Equal(r, []string{CancelledMessage}) {
		t.Errorf("history results = %q", r)
	}
	if _, _, interrupted := s.TurnState(); !interrupted {
		t.Error("cancelled turn not marked interrupted")
	}

	// The approval gate cancelled at the same point.
	asrv := providertest.New(fakeResp{ToolCalls: []fakeCall{{ID: "c1", Name: "bash", Arguments: `{"command":"true"}`}}},
		fakeResp{Text: "follow-up"})
	defer asrv.Close()
	a, _ := newLoopSession(t, asrv)
	if err := a.Submit("run"); err != nil {
		t.Fatal(err)
	}
	untilKind(t, a, EvApproval)
	aEvs := cancelAndCollect(t, a)

	kinds := func(evs []Event) []EventKind {
		var out []EventKind
		for _, e := range evs {
			out = append(out, e.Kind)
		}
		return out
	}
	if !slices.Equal(kinds(qEvs), kinds(aEvs)) {
		t.Errorf("question cancel events %+v, approval cancel events %+v", qEvs, aEvs)
	}
	if !findEvent(qEvs, EvInfo, CancelledMessage) {
		t.Errorf("no cancelled info after question cancel: %+v", qEvs)
	}
}

func TestQuestionOptionsCappedAtSix(t *testing.T) {
	srv := questionServer(`{"question":"many","options":["1","2","3","4","5","6","7","8"]}`)
	defer srv.Close()
	s, _ := newLoopSession(t, srv)
	var got []string
	evs := runQuestionTurn(t, s, func(r *QuestionRequest) {
		got = append([]string(nil), r.Options...)
		r.Respond("6")
	})
	if !slices.Equal(got, []string{"1", "2", "3", "4", "5", "6"}) {
		t.Errorf("options = %q, want the first 6", got)
	}
	if rs := toolResults(evs, "question"); len(rs) != 1 || rs[0].IsError || rs[0].Text != "6" {
		t.Errorf("too many options must not be an error: %+v", rs)
	}
	assertContinued(t, s, evs)
}

func TestQuestionRespondOnceOnly(t *testing.T) {
	srv := questionServer(`{"question":"one?","options":["first","second"]}`)
	defer srv.Close()
	s, _ := newLoopSession(t, srv)
	evs := runQuestionTurn(t, s, func(r *QuestionRequest) {
		r.Respond("first")
		r.Respond("second")
		r.Cancel()
	})
	if r := questionResults(s); !slices.Equal(r, []string{"first"}) {
		t.Errorf("history results = %q, want exactly [first]", r)
	}
	assertContinued(t, s, evs)
}
