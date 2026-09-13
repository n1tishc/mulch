package session

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"time"

	"github.com/n1tishc/mulch/internal/bus"
	"github.com/n1tishc/mulch/internal/event"
	"github.com/n1tishc/mulch/internal/hook"
)

var ErrNotRunning = errors.New("session is not running")

type RunOpts struct{ Workdir string }
type Runner func(context.Context, string, string, RunOpts, *Steering) error

type Options struct {
	MaxSessions int
	Bus         *bus.Bus
	Run         Runner
	Resume      Runner
	// Prepare validates and captures per-task settings synchronously, before
	// launch. When set, it supplies the runner for both new and resumed tasks.
	Prepare func(resume bool) (Runner, error)
	Close   func() error
}

type Status struct {
	ID, Task string
	Started  time.Time
	Done     bool
	Err      error
}

type owned struct {
	status   Status
	cancel   context.CancelFunc
	steering *Steering
	done     chan struct{}
}

type Manager struct {
	ctx          context.Context
	cancel       context.CancelFunc
	run          Runner
	resume       Runner
	prepare      func(bool) (Runner, error)
	close        func() error
	bus          *bus.Bus
	limit        chan struct{}
	lifeMu       sync.Mutex
	closed       bool
	mu           sync.RWMutex
	sessions     map[string]*owned
	wg           sync.WaitGroup
	subMu        sync.Mutex
	subWG        sync.WaitGroup
	resourceOnce sync.Once
	closeErr     error
}

func New(opts Options) *Manager {
	if opts.MaxSessions <= 0 {
		opts.MaxSessions = 32
	}
	if opts.Bus == nil {
		opts.Bus = bus.New()
	}
	ctx, cancel := context.WithCancel(context.Background())
	return &Manager{ctx: ctx, cancel: cancel, run: opts.Run, resume: opts.Resume, prepare: opts.Prepare, close: opts.Close, bus: opts.Bus, limit: make(chan struct{}, opts.MaxSessions), sessions: make(map[string]*owned)}
}

func NewBus() *bus.Bus { return bus.New() }

func (m *Manager) Start(ctx context.Context, task string) (string, error) {
	return m.StartWith(ctx, task, RunOpts{})
}

func (m *Manager) StartWith(ctx context.Context, task string, opts RunOpts) (string, error) {
	runner := m.run
	if m.prepare != nil {
		var err error
		runner, err = m.prepare(false)
		if err != nil {
			return "", err
		}
	}
	id, err := event.NewSessionID()
	if err != nil {
		return "", err
	}
	return m.launch(ctx, id, task, opts, runner)
}

func (m *Manager) ResumeWith(ctx context.Context, id, task string, opts RunOpts) (string, error) {
	runner := m.resume
	if m.prepare != nil {
		var err error
		runner, err = m.prepare(true)
		if err != nil {
			return "", err
		}
	}
	if runner == nil {
		return "", errors.New("session manager cannot resume sessions")
	}
	return m.launch(ctx, id, task, opts, runner)
}

func (m *Manager) launch(ctx context.Context, id, task string, opts RunOpts, runner Runner) (string, error) {
	select {
	case m.limit <- struct{}{}:
	case <-ctx.Done():
		return "", ctx.Err()
	case <-m.ctx.Done():
		return "", errors.New("session manager closed")
	}
	runCtx, cancel := context.WithCancel(m.ctx)
	stopCallerCancel := context.AfterFunc(ctx, cancel)
	entry := &owned{status: Status{ID: id, Task: task, Started: time.Now()}, cancel: cancel, steering: &Steering{}, done: make(chan struct{})}
	m.lifeMu.Lock()
	if m.closed {
		m.lifeMu.Unlock()
		stopCallerCancel()
		cancel()
		<-m.limit
		return "", errors.New("session manager closed")
	}
	m.mu.Lock()
	if previous, ok := m.sessions[id]; ok && !previous.status.Done {
		m.mu.Unlock()
		m.lifeMu.Unlock()
		stopCallerCancel()
		cancel()
		<-m.limit
		return "", errors.New("session is already running")
	}
	m.sessions[id] = entry
	m.mu.Unlock()
	m.wg.Add(1)
	m.lifeMu.Unlock()
	go func() {
		defer m.wg.Done()
		defer func() { <-m.limit }()
		defer stopCallerCancel()
		err := runner(runCtx, id, task, opts, entry.steering)
		m.mu.Lock()
		entry.status.Done, entry.status.Err = true, err
		close(entry.done)
		m.mu.Unlock()
	}()
	return id, nil
}

func (m *Manager) Wait(ctx context.Context, id string) error {
	m.mu.RLock()
	entry, ok := m.sessions[id]
	m.mu.RUnlock()
	if !ok {
		return ErrNotRunning
	}
	select {
	case <-entry.done:
		m.mu.RLock()
		err := entry.status.Err
		m.mu.RUnlock()
		return err
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (m *Manager) Steer(id, text string) error {
	m.mu.RLock()
	entry, ok := m.sessions[id]
	running := ok && !entry.status.Done
	m.mu.RUnlock()
	if !running {
		return ErrNotRunning
	}
	entry.steering.Add(text)
	return nil
}

func (m *Manager) Cancel(id string) error {
	m.mu.RLock()
	entry, ok := m.sessions[id]
	running := ok && !entry.status.Done
	m.mu.RUnlock()
	if !running {
		return ErrNotRunning
	}
	entry.cancel()
	return nil
}

func (m *Manager) Subscribe(ctx context.Context, id string) (<-chan event.Event, error) {
	m.mu.RLock()
	_, ok := m.sessions[id]
	m.mu.RUnlock()
	if !ok {
		return nil, ErrNotRunning
	}
	m.subMu.Lock()
	select {
	case <-m.ctx.Done():
		m.subMu.Unlock()
		return nil, errors.New("session manager closed")
	default:
	}
	subCtx, cancel := context.WithCancel(m.ctx)
	context.AfterFunc(ctx, cancel)
	source := m.bus.Subscribe(subCtx, id, 64)
	out := make(chan event.Event, 64)
	m.subWG.Add(1)
	m.subMu.Unlock()
	go func() {
		defer m.subWG.Done()
		defer close(out)
		for candidate := range source {
			select {
			case out <- candidate:
			case <-subCtx.Done():
				return
			}
		}
	}()
	return out, nil
}

func (m *Manager) Status(id string) (Status, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	x, ok := m.sessions[id]
	if !ok {
		return Status{}, false
	}
	return x.status, true
}
func (m *Manager) List() []Status {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]Status, 0, len(m.sessions))
	for _, x := range m.sessions {
		out = append(out, x.status)
	}
	return out
}

func (m *Manager) Shutdown(ctx context.Context) error {
	m.lifeMu.Lock()
	m.closed = true
	m.subMu.Lock()
	m.cancel()
	m.subMu.Unlock()
	m.lifeMu.Unlock()
	done := make(chan struct{})
	go func() { m.wg.Wait(); m.subWG.Wait(); close(done) }()
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (m *Manager) Close() error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := m.Shutdown(ctx); err != nil {
		return err
	}
	m.resourceOnce.Do(func() {
		if m.close != nil {
			m.closeErr = m.close()
		}
	})
	return m.closeErr
}

type appendStore interface {
	Append(context.Context, event.Event) (event.Event, error)
}
type Steering struct {
	mu      sync.Mutex
	pending []string
	store   appendStore
}

func (s *Steering) Bind(store appendStore) *Steering { s.store = store; return s }
func (s *Steering) Name() string                     { return "steer" }
func (s *Steering) Add(text string)                  { s.mu.Lock(); s.pending = append(s.pending, text); s.mu.Unlock() }
func (s *Steering) Drain() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := append([]string(nil), s.pending...)
	s.pending = nil
	return out
}
func (s *Steering) BeforeTurn(ctx context.Context, turn *hook.Turn) error {
	for _, text := range s.Drain() {
		payload, err := json.Marshal(event.UserMessage{Text: text, Origin: "steer"})
		if err != nil {
			return err
		}
		if s.store == nil {
			return errors.New("steering event store is not configured")
		}
		if _, err = s.store.Append(context.WithoutCancel(ctx), event.Event{SessionID: turn.SessionID, Turn: turn.Turn, Type: event.TypeUserMessage, Payload: payload, Visible: true}); err != nil {
			return err
		}
	}
	return nil
}
