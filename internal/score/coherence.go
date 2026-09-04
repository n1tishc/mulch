package score

import (
	"context"
	"encoding/json"
	"errors"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/n1tishc/mulch/internal/event"
	"github.com/n1tishc/mulch/internal/provider"
)

const coherenceChunkBytes = 2000

type ContradictoryPair struct {
	LeftSeq  int64  `json:"left_seq"`
	RightSeq int64  `json:"right_seq"`
	Reason   string `json:"reason,omitempty"`
}

type Coherence struct {
	judge     provider.LLM
	model     string
	mu        sync.Mutex
	requested bool
	last      Result
}

func NewCoherence(judge provider.LLM) *Coherence {
	return NewCoherenceWithModel(judge, "")
}
func NewCoherenceWithModel(judge provider.LLM, model string) *Coherence {
	return &Coherence{judge: judge, model: model, last: Result{Score: 1}}
}
func (*Coherence) Name() string            { return "coherence" }
func (*Coherence) Deadline() time.Duration { return 8 * time.Second }
func (c *Coherence) Request()              { c.mu.Lock(); c.requested = true; c.mu.Unlock() }

func (c *Coherence) Score(ctx context.Context, input Input) (Result, error) {
	c.mu.Lock()
	due := input.Turn%3 == 0 || c.requested
	c.requested = false
	last := c.last
	c.mu.Unlock()
	if !due {
		return last, nil
	}
	if c.judge == nil {
		return Result{}, errors.New("coherence judge is not configured")
	}
	pairs := coherencePairs(input.Visible)
	if len(pairs) == 0 {
		return Result{Score: 1}, nil
	}
	body, _ := json.Marshal(map[string]any{"instruction": "Classify each pair as consistent, contradictory, or unrelated. Return strict JSON only.", "pairs": pairs})
	out := make(chan provider.Delta, 64)
	type judgedResponse struct {
		response provider.Response
		err      error
	}
	done := make(chan judgedResponse, 1)
	go func() {
		model := c.model
		if model == "" {
			model = input.Session.Model
		}
		response, err := c.judge.Stream(ctx, provider.Request{Model: model, MaxTokens: 1000, Messages: []provider.Message{{Role: provider.RoleUser, Blocks: []provider.Block{{Type: "text", Text: string(body)}}}}}, out)
		close(out)
		done <- judgedResponse{response, err}
	}()
	for range out {
	}
	completed := <-done
	response, err := completed.response, completed.err
	if err != nil {
		return Result{}, err
	}
	var raw strings.Builder
	for _, block := range response.Blocks {
		if block.Type == "text" {
			raw.WriteString(block.Text)
		}
	}
	var judged struct {
		Pairs []struct {
			LeftSeq  int64  `json:"left_seq"`
			RightSeq int64  `json:"right_seq"`
			Verdict  string `json:"verdict"`
			Reason   string `json:"reason"`
		} `json:"pairs"`
	}
	if err := json.Unmarshal([]byte(raw.String()), &judged); err != nil {
		return Result{}, err
	}
	consistent, contradictory := 0, 0
	var contradictions []ContradictoryPair
	for _, pair := range judged.Pairs {
		switch pair.Verdict {
		case "consistent":
			consistent++
		case "contradictory":
			contradictory++
			contradictions = append(contradictions, ContradictoryPair{pair.LeftSeq, pair.RightSeq, pair.Reason})
		}
	}
	score := 1.0
	if consistent+contradictory > 0 {
		score = 1 - float64(contradictory)/float64(consistent+contradictory)
	}
	result := Result{Score: score, Details: map[string]any{"contradictory_pairs": contradictions}}
	c.mu.Lock()
	c.last = result
	c.mu.Unlock()
	return result, nil
}

type coherencePair struct {
	LeftSeq  int64  `json:"left_seq"`
	Left     string `json:"left"`
	RightSeq int64  `json:"right_seq"`
	Right    string `json:"right"`
	affinity int
}

func coherencePairs(events []event.Event) []coherencePair {
	type chunk struct {
		seq   int64
		text  string
		words map[string]bool
	}
	var chunks []chunk
	for _, candidate := range events {
		text := coherenceText(candidate)
		if text == "" {
			continue
		}
		if len(text) > coherenceChunkBytes {
			text = text[:coherenceChunkBytes]
			for !utf8.ValidString(text) {
				text = text[:len(text)-1]
			}
		}
		words := map[string]bool{}
		for _, word := range strings.FieldsFunc(strings.ToLower(text), func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsDigit(r) && r != '.' && r != '/' }) {
			if len(word) > 2 {
				words[word] = true
			}
		}
		chunks = append(chunks, chunk{candidate.Seq, text, words})
	}
	var pairs []coherencePair
	for i := 0; i < len(chunks); i++ {
		for j := i + 1; j < len(chunks); j++ {
			affinity := 0
			for w := range chunks[i].words {
				if chunks[j].words[w] {
					affinity++
				}
			}
			pairs = append(pairs, coherencePair{chunks[i].seq, chunks[i].text, chunks[j].seq, chunks[j].text, affinity})
		}
	}
	sort.SliceStable(pairs, func(i, j int) bool { return pairs[i].affinity > pairs[j].affinity })
	if len(pairs) > 6 {
		pairs = pairs[:6]
	}
	return pairs
}
func coherenceText(candidate event.Event) string {
	switch candidate.Type {
	case event.TypeAssistantMessage:
		var p event.AssistantMessage
		if candidate.Decode(&p) == nil {
			return p.Text
		}
	case event.TypeToolResult:
		var p event.ToolResult
		if candidate.Decode(&p) == nil {
			return p.Output
		}
	case event.TypeContextInject:
		var p event.ContextInject
		if candidate.Decode(&p) == nil {
			return p.Text
		}
	case event.TypeContextCompact:
		var p event.ContextCompact
		if candidate.Decode(&p) == nil {
			return p.Summary
		}
	}
	return ""
}
