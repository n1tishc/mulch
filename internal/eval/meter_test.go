package eval

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/n1tishc/mulch/internal/provider"
)

type meterFake struct {
	fail  bool
	calls int
	mu    sync.Mutex
}

func (f *meterFake) Stream(_ context.Context, req provider.Request, _ chan<- provider.Delta) (provider.Response, error) {
	f.mu.Lock()
	f.calls++
	f.mu.Unlock()
	if req.MaxTokens != 2048 || !req.NoRetry {
		return provider.Response{}, errors.New("missing request bounds")
	}
	if f.fail {
		return provider.Response{}, errors.New("transport failed")
	}
	return provider.Response{InputTokens: 10, OutputTokens: 5}, nil
}
func TestMeterReservesBeforeConcurrentRequestsAndChargesFailures(t *testing.T) {
	m := &meter{limit: 4000}
	f := &meterFake{fail: true}
	var wg sync.WaitGroup
	for range 8 {
		wg.Add(1)
		go func() { defer wg.Done(); _, _ = m.wrap(f, "candidate").Stream(t.Context(), provider.Request{}, nil) }()
	}
	wg.Wait()
	if f.calls != 1 {
		t.Fatalf("calls=%d, budget admitted more than one request", f.calls)
	}
	usage, charged := m.snapshot()
	if charged < 3000 || usage["candidate"].FailedCalls != 1 {
		t.Fatalf("usage=%v charged=%d", usage, charged)
	}
}
func TestMeterAccountsForEveryRole(t *testing.T) {
	m := &meter{limit: 4000}
	f := &meterFake{}
	for _, role := range []string{"agent", "judge", "summary", "candidate-agent"} {
		if _, err := m.wrap(f, role).Stream(t.Context(), provider.Request{}, nil); err != nil {
			t.Fatal(err)
		}
	}
	usage, charged := m.snapshot()
	if charged != 60 || len(usage) != 4 {
		t.Fatalf("usage=%v charged=%d", usage, charged)
	}
}
