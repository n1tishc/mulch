package runtime

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/n1tishc/mulch/internal/event"
	"github.com/n1tishc/mulch/internal/intervene"
	"github.com/n1tishc/mulch/internal/provider"
	"github.com/n1tishc/mulch/internal/score"
)

type lowCoherence struct{}

func (lowCoherence) Name() string            { return "coherence" }
func (lowCoherence) Deadline() time.Duration { return time.Second }
func (lowCoherence) Score(context.Context, score.Input) (score.Result, error) {
	return score.Result{Score: .1}, nil
}

type runtimeLLM struct {
	store    *event.SQLiteStore
	id, role string
	calls    int
}

func (l *runtimeLLM) Stream(ctx context.Context, req provider.Request, _ chan<- provider.Delta) (provider.Response, error) {
	l.calls++
	if strings.HasPrefix(l.role, "candidate-") {
		return provider.Response{InputTokens: 10, OutputTokens: 10, Blocks: []provider.Block{{Type: "tool_use", CallID: "candidate-write", Name: "write", Input: `{"path":"candidate.txt","content":"isolated"}`}}}, nil
	}
	if l.calls > 1 {
		for {
			es, err := l.store.List(ctx, l.id, 1)
			if err != nil {
				return provider.Response{}, err
			}
			ready := false
			for _, e := range es {
				if e.Type == event.TypeScoreHealth && e.Turn == l.calls-1 {
					ready = true
				}
			}
			if ready {
				break
			}
			select {
			case <-ctx.Done():
				return provider.Response{}, ctx.Err()
			case <-time.After(time.Millisecond):
			}
		}
	}
	if l.calls > 3 {
		return provider.Response{InputTokens: 10, OutputTokens: 1, Blocks: []provider.Block{{Type: "text", Text: "done"}}}, nil
	}
	return provider.Response{InputTokens: 10, OutputTokens: 1, Blocks: []provider.Block{{Type: "tool_use", CallID: "read", Name: "read", Input: `{"path":"SPEC.md"}`}}}, nil
}
func TestSharedRuntimeRacesWithoutMergingCandidateWrites(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	router := &Router{}
	store, err := event.Open(ctx, filepath.Join(t.TempDir(), "trace.db"), router)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	wd := t.TempDir()
	if err := os.WriteFile(filepath.Join(wd, "SPEC.md"), []byte("preserve original task"), 0600); err != nil {
		t.Fatal(err)
	}
	config := Config{Store: store, Router: router, Mode: Race, Policy: intervene.DefaultPolicy(), Weights: score.Weights{Coherence: .7, Saturation: .3}, Model: "fake", ContextWindow: 200000, MaxTurns: 5,
		LLM: func(id, role string) provider.LLM { return &runtimeLLM{store: store, id: id, role: role} }, Scorers: func(*score.Coherence) []score.Scorer { return []score.Scorer{lowCoherence{}, score.Saturation{}} }}
	if err := config.Execute(ctx, event.Session{ID: "parent", Task: "test", Workdir: wd, Model: "fake", ContextWindow: 200000}, false, "test", nil, nil); err != nil {
		t.Fatal(err)
	}
	es, err := store.List(ctx, "parent", 1)
	if err != nil {
		t.Fatal(err)
	}
	races := 0
	for _, e := range es {
		if e.Type != event.TypeRaceEnd {
			continue
		}
		races++
		var end event.RaceEnd
		if err := json.Unmarshal(e.Payload, &end); err != nil {
			t.Fatal(err)
		}
		for _, id := range end.Branches {
			branch, err := store.List(ctx, id, 1)
			if err != nil {
				t.Fatal(err)
			}
			scored := false
			for _, e := range branch {
				scored = scored || e.Type == event.TypeScoreHealth
			}
			if !scored {
				t.Fatal("candidate was not scored")
			}
		}
	}
	if races != 1 {
		t.Fatalf("races=%d", races)
	}
	if _, err := os.Stat(filepath.Join(wd, "candidate.txt")); !os.IsNotExist(err) {
		t.Fatal("candidate write escaped its copy")
	}
	router.mu.RLock()
	remaining := len(router.listeners)
	router.mu.RUnlock()
	if remaining != 0 {
		t.Fatalf("retained %d session listeners", remaining)
	}
}
