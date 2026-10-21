package agent

import (
	"context"
	"sync"
)

// EventKind is the type of a session event.
type EventKind int

// Event kinds.
const (
	EvText EventKind = iota + 1
	EvToolCall
	EvToolResult
	EvApproval
	EvError
	EvInfo
	EvTurnDone
	EvSubagent
	EvQuestion // the question tool waits for the user's answer; see Event.Question
)

// Event is one thing that happened in a session, reported to the UI.
type Event struct {
	Kind     EventKind
	AgentID  string           // MainAgentID or subagent id
	Text     string           // text delta, tool result output, error or info message
	Tool     string           // EvToolCall / EvToolResult / EvApproval
	Args     string           // EvToolCall: raw JSON arguments
	IsError  bool             // EvToolResult
	Approval *ApprovalRequest // EvApproval
	Question *QuestionRequest // EvQuestion
}

// eventQueue is an unbounded FIFO of events. Pushing never blocks.
type eventQueue struct {
	mu    sync.Mutex
	items []Event
	wake  chan struct{} // buffered 1: signals that items may be non-empty
}

func newEventQueue() *eventQueue {
	return &eventQueue{wake: make(chan struct{}, 1)}
}

// push appends ev and wakes a waiting reader. It never blocks.
func (q *eventQueue) push(ev Event) {
	q.mu.Lock()
	q.items = append(q.items, ev)
	q.mu.Unlock()
	select {
	case q.wake <- struct{}{}:
	default:
	}
}

// pop removes and returns the oldest event, if any.
func (q *eventQueue) pop() (Event, bool) {
	q.mu.Lock()
	defer q.mu.Unlock()
	if len(q.items) == 0 {
		return Event{}, false
	}
	ev := q.items[0]
	q.items[0] = Event{}
	q.items = q.items[1:]
	if len(q.items) == 0 {
		q.items = nil
	}
	return ev, true
}

// drain removes and returns all queued events.
func (q *eventQueue) drain() []Event {
	q.mu.Lock()
	defer q.mu.Unlock()
	out := q.items
	q.items = nil
	return out
}

// next blocks until an event is available or ctx is done.
func (q *eventQueue) next(ctx context.Context) (Event, error) {
	for {
		if ev, ok := q.pop(); ok {
			return ev, nil
		}
		select {
		case <-q.wake:
		case <-ctx.Done():
			// An event may have arrived together with the cancellation.
			if ev, ok := q.pop(); ok {
				return ev, nil
			}
			return Event{}, ctx.Err()
		}
	}
}

// emit redacts the event's text fields and queues it. It never blocks.
func (s *Session) emit(ev Event) {
	ev.Text = s.Redact(ev.Text)
	ev.Args = s.Redact(ev.Args)
	s.events.push(ev)
}

// NextEvent blocks until an event is available or ctx is done.
func (s *Session) NextEvent(ctx context.Context) (Event, error) {
	return s.events.next(ctx)
}

// PollEvents takes all queued events. It never blocks.
func (s *Session) PollEvents() []Event {
	return s.events.drain()
}
