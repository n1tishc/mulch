package tool

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"
)

type Call struct {
	ID    string
	Name  string
	Input json.RawMessage
}

type Outcome struct {
	Call      Call
	Result    Result
	Duration  time.Duration
	Cancelled bool
	TimedOut  bool
}

type EventKind string

const (
	EventStart  EventKind = "start"
	EventResult EventKind = "result"
)

type ExecutionEvent struct {
	Kind      EventKind
	Call      Call
	Result    Result
	StartedAt time.Time
	Duration  time.Duration
	Cancelled bool
	TimedOut  bool
}

type Executor struct {
	tools   map[string]Tool
	MaxPar  int
	Timeout time.Duration
}

func NewExecutor(tools []Tool) *Executor {
	byName := make(map[string]Tool, len(tools))
	for _, candidate := range tools {
		byName[candidate.Name()] = candidate
	}
	return &Executor{tools: byName, MaxPar: 8, Timeout: DefaultBashTimeout}
}

func (e *Executor) Specs() []Tool {
	result := make([]Tool, 0, len(e.tools))
	for _, name := range []string{"read", "write", "edit", "bash"} {
		if candidate := e.tools[name]; candidate != nil {
			result = append(result, candidate)
		}
	}
	return result
}

func (e *Executor) RunAll(ctx context.Context, calls []Call, emit func(ExecutionEvent)) []Outcome {
	outcomes := make([]Outcome, len(calls))
	maxPar := e.MaxPar
	if maxPar <= 0 {
		maxPar = 8
	}
	timeout := e.Timeout
	if timeout <= 0 {
		timeout = DefaultBashTimeout
	}
	sem := make(chan struct{}, maxPar)
	var wg sync.WaitGroup
	var emitMu sync.Mutex
	send := func(event ExecutionEvent) {
		if emit != nil {
			emitMu.Lock()
			emit(event)
			emitMu.Unlock()
		}
	}
	for i, call := range calls {
		i, call := i, call
		wg.Add(1)
		go func() {
			defer wg.Done()
			select {
			case sem <- struct{}{}:
			case <-ctx.Done():
				started := time.Now()
				send(ExecutionEvent{Kind: EventStart, Call: call, StartedAt: started})
				outcomes[i] = Outcome{Call: call, Result: Result{Output: "cancelled", IsError: true}, Duration: time.Since(started), Cancelled: true}
				send(ExecutionEvent{Kind: EventResult, Call: call, Result: outcomes[i].Result, Duration: outcomes[i].Duration, Cancelled: true})
				return
			}
			defer func() { <-sem }()
			started := time.Now()
			send(ExecutionEvent{Kind: EventStart, Call: call, StartedAt: started})
			toolCtx, cancel := context.WithTimeout(ctx, timeout)
			result, err := Result{}, error(nil)
			if candidate := e.tools[call.Name]; candidate == nil {
				result = Result{Output: fmt.Sprintf("unknown tool %q", call.Name), IsError: true}
			} else {
				result, err = candidate.Run(toolCtx, call.Input)
			}
			cancelled := ctx.Err() != nil
			timedOut := !cancelled && toolCtx.Err() == context.DeadlineExceeded
			cancel()
			if err != nil {
				result = Result{Output: err.Error(), IsError: true}
			}
			duration := time.Since(started)
			outcomes[i] = Outcome{Call: call, Result: result, Duration: duration, Cancelled: cancelled, TimedOut: timedOut}
			send(ExecutionEvent{Kind: EventResult, Call: call, Result: result, Duration: duration, Cancelled: cancelled, TimedOut: timedOut})
		}()
	}
	wg.Wait()
	return outcomes
}
