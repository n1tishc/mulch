package eval

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"time"

	"github.com/n1tishc/mulch/internal/provider"
)

var ErrBudget = errors.New("evaluation token budget exhausted")

type Usage struct {
	Role           string `json:"role"`
	Calls          int    `json:"calls"`
	InputTokens    int    `json:"input_tokens"`
	OutputTokens   int    `json:"output_tokens"`
	FailedCalls    int    `json:"failed_calls"`
	ReservedTokens int    `json:"reserved_tokens"`
	WallMS         int64  `json:"wall_ms"`
}
type meter struct {
	pending        sync.WaitGroup
	closed         bool
	mu             sync.Mutex
	limit, charged int
	usage          map[string]Usage
}
type meteredLLM struct {
	base  provider.LLM
	meter *meter
	role  string
}

func (m *meter) close() { m.mu.Lock(); m.closed = true; m.mu.Unlock(); m.pending.Wait() }

func (m *meter) wrap(base provider.LLM, role string) provider.LLM { return &meteredLLM{base, m, role} }
func (m *meter) snapshot() (map[string]Usage, int) {
	m.mu.Lock()
	defer m.mu.Unlock()
	result := map[string]Usage{}
	for k, v := range m.usage {
		result[k] = v
	}
	return result, m.charged
}
func (l *meteredLLM) Stream(ctx context.Context, req provider.Request, out chan<- provider.Delta) (provider.Response, error) {
	req.NoRetry = true
	if req.MaxTokens <= 0 || req.MaxTokens > 2048 {
		req.MaxTokens = 2048
	}
	// UTF-8 byte count plus generous message/tool framing is a conservative
	// preflight bound for these text-only requests, not a tokenizer estimate.
	body, err := json.Marshal(struct {
		Messages []provider.Message
		Tools    []provider.ToolSpec
	}{req.Messages, req.Tools})
	if err != nil {
		return provider.Response{}, err
	}
	reservation := len(body) + 1024 + req.MaxTokens
	m := l.meter
	m.mu.Lock()
	if m.closed || ctx.Err() != nil || (m.limit > 0 && m.charged+reservation > m.limit) {
		m.mu.Unlock()
		return provider.Response{}, ErrBudget
	}
	m.charged += reservation
	m.pending.Add(1)
	m.mu.Unlock()
	defer m.pending.Done()
	start := time.Now()
	response, err := l.base.Stream(ctx, req, out)
	m.mu.Lock()
	defer m.mu.Unlock()
	actual := response.InputTokens + response.OutputTokens
	// Failed/usage-less requests retain their full reservation; never assume free.
	charged := reservation
	if err == nil && actual > 0 {
		charged = actual
		m.charged += actual - reservation
	}
	if m.usage == nil {
		m.usage = map[string]Usage{}
	}
	u := m.usage[l.role]
	u.Role = l.role
	u.Calls++
	u.InputTokens += response.InputTokens
	u.OutputTokens += response.OutputTokens
	u.ReservedTokens += charged
	u.WallMS += time.Since(start).Milliseconds()
	if err != nil {
		u.FailedCalls++
	}
	m.usage[l.role] = u
	return response, err
}
