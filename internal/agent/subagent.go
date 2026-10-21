package agent

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"sync"
)

// SubStatus is the lifecycle state of a subagent.
type SubStatus string

// Subagent states.
const (
	SubRunning   SubStatus = "running"
	SubDone      SubStatus = "done"
	SubFailed    SubStatus = "failed"
	SubCancelled SubStatus = "cancelled"
)

// SubInfo is a snapshot of one subagent.
type SubInfo struct {
	ID, Task, Model string
	Status          SubStatus
	Tokens          int
	Final, Err      string // final text (done) or error text (failed/cancelled)
}

// Subagent is the handle passed to a RunFunc.
type Subagent struct {
	m  *Manager
	id string
}

// ID returns the subagent's id.
func (a *Subagent) ID() string { return a.id }

// AddTokens adds n tokens to the subagent's usage.
func (a *Subagent) AddTokens(n int) {
	a.m.mu.Lock()
	e, ok := a.m.subs[a.id]
	if !ok {
		a.m.mu.Unlock()
		return
	}
	e.info.Tokens += n
	info := e.info
	a.m.mu.Unlock()
	a.m.notify(info)
}

// Log appends one transcript line.
func (a *Subagent) Log(line string) {
	a.m.mu.Lock()
	if e, ok := a.m.subs[a.id]; ok {
		e.transcript = append(e.transcript, line)
	}
	a.m.mu.Unlock()
}

// RunFunc executes one subagent's task and returns its final text.
type RunFunc func(ctx context.Context, a *Subagent, task, model string) (string, error)

type subEntry struct {
	info       SubInfo
	transcript []string
	cancel     context.CancelFunc
	done       chan struct{}
}

// Manager starts, tracks and cancels subagents. It is safe for concurrent use.
type Manager struct {
	max      int
	run      RunFunc
	onChange func(SubInfo)

	mu      sync.Mutex
	next    int
	running int
	order   []string
	subs    map[string]*subEntry
}

// NewManager returns a manager allowing at most max concurrently running
// subagents (max <= 0 means 4). onChange may be nil.
func NewManager(max int, run RunFunc, onChange func(SubInfo)) *Manager {
	if max <= 0 {
		max = 4
	}
	return &Manager{max: max, run: run, onChange: onChange, subs: map[string]*subEntry{}}
}

func (m *Manager) notify(info SubInfo) {
	if m.onChange != nil {
		m.onChange(info)
	}
}

// Spawn starts a subagent for task and returns its id immediately.
func (m *Manager) Spawn(parent context.Context, task, model string) (string, error) {
	if parent == nil {
		parent = context.Background()
	}
	m.mu.Lock()
	if m.running >= m.max {
		m.mu.Unlock()
		return "", fmt.Errorf("subagent limit reached (max %d)", m.max)
	}
	m.next++
	id := "a" + strconv.Itoa(m.next)
	ctx, cancel := context.WithCancel(parent)
	e := &subEntry{
		info:   SubInfo{ID: id, Task: task, Model: model, Status: SubRunning},
		cancel: cancel,
		done:   make(chan struct{}),
	}
	m.subs[id] = e
	m.order = append(m.order, id)
	m.running++
	info := e.info
	m.mu.Unlock()

	m.notify(info)
	go m.execute(ctx, e, &Subagent{m: m, id: id}, task, model)
	return id, nil
}

func (m *Manager) execute(ctx context.Context, e *subEntry, a *Subagent, task, model string) {
	final, panicked, err := m.safeRun(ctx, a, task, model)
	ctxErr := ctx.Err()
	e.cancel()

	m.mu.Lock()
	switch {
	case panicked:
		e.info.Status = SubFailed
		e.info.Err = err.Error()
	case err == nil:
		e.info.Status = SubDone
		e.info.Final = final
	case ctxErr != nil:
		e.info.Status = SubCancelled
		e.info.Err = "cancelled"
	default:
		e.info.Status = SubFailed
		e.info.Err = err.Error()
	}
	m.running--
	info := e.info
	close(e.done)
	m.mu.Unlock()

	m.notify(info)
}

func (m *Manager) safeRun(ctx context.Context, a *Subagent, task, model string) (final string, panicked bool, err error) {
	defer func() {
		if r := recover(); r != nil {
			final, panicked, err = "", true, fmt.Errorf("subagent panic: %v", r)
		}
	}()
	if m.run == nil {
		return "", false, errors.New("no run function")
	}
	final, err = m.run(ctx, a, task, model)
	return final, false, err
}

// Wait blocks until subagent id has finished or ctx is done.
func (m *Manager) Wait(ctx context.Context, id string) (string, error) {
	m.mu.Lock()
	e, ok := m.subs[id]
	m.mu.Unlock()
	if !ok {
		return "", fmt.Errorf("unknown subagent %q", id)
	}
	select {
	case <-e.done:
	case <-ctx.Done():
		return "", ctx.Err()
	}
	m.mu.Lock()
	info := e.info
	m.mu.Unlock()
	switch info.Status {
	case SubDone:
		return info.Final, nil
	case SubCancelled:
		return "", fmt.Errorf("subagent %s cancelled", id)
	default:
		return "", fmt.Errorf("subagent %s failed: %s", id, info.Err)
	}
}

// Cancel cancels subagent id. Unknown ids are an error; finished ones are a no-op.
func (m *Manager) Cancel(id string) error {
	m.mu.Lock()
	e, ok := m.subs[id]
	m.mu.Unlock()
	if !ok {
		return fmt.Errorf("unknown subagent %q", id)
	}
	e.cancel()
	return nil
}

// CancelAll cancels every running subagent.
func (m *Manager) CancelAll() {
	m.mu.Lock()
	var cancels []context.CancelFunc
	for _, e := range m.subs {
		if e.info.Status == SubRunning {
			cancels = append(cancels, e.cancel)
		}
	}
	m.mu.Unlock()
	for _, c := range cancels {
		c()
	}
}

// List returns snapshots of all subagents in spawn order.
func (m *Manager) List() []SubInfo {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]SubInfo, 0, len(m.order))
	for _, id := range m.order {
		out = append(out, m.subs[id].info)
	}
	return out
}

// Get returns a snapshot of subagent id.
func (m *Manager) Get(id string) (SubInfo, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	e, ok := m.subs[id]
	if !ok {
		return SubInfo{}, false
	}
	return e.info, true
}

// Transcript returns a copy of the lines logged by subagent id, in order.
func (m *Manager) Transcript(id string) ([]string, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	e, ok := m.subs[id]
	if !ok {
		return nil, false
	}
	return append([]string(nil), e.transcript...), true
}

// Running returns the number of currently running subagents.
func (m *Manager) Running() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.running
}
