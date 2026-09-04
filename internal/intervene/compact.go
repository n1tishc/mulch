package intervene

import (
	"context"
	"encoding/json"
	"errors"
	"strings"

	"github.com/n1tishc/mulch/internal/event"
	"github.com/n1tishc/mulch/internal/provider"
)

type LLMSummarizer struct {
	llm   provider.LLM
	model string
}

func NewLLMSummarizer(llm provider.LLM, model string) *LLMSummarizer {
	return &LLMSummarizer{llm: llm, model: model}
}

func (s *LLMSummarizer) Summarize(ctx context.Context, events []event.Event) (string, int, error) {
	encoded, err := json.Marshal(events)
	if err != nil {
		return "", 0, err
	}
	prompt := "Summarize this old agent context compactly. Preserve every file path, decision, and open question. Do not invent facts. Use headings Paths, Decisions, Open questions, and Other context.\n\n" + string(encoded)
	deltas := make(chan provider.Delta, 32)
	type result struct {
		response provider.Response
		err      error
	}
	done := make(chan result, 1)
	go func() {
		response, streamErr := s.llm.Stream(ctx, provider.Request{Model: s.model, MaxTokens: 1200, Messages: []provider.Message{{Role: provider.RoleUser, Blocks: []provider.Block{{Type: "text", Text: prompt}}}}}, deltas)
		close(deltas)
		done <- result{response, streamErr}
	}()
	var text strings.Builder
	for delta := range deltas {
		text.WriteString(delta.Text)
	}
	got := <-done
	if got.err != nil {
		return "", 0, got.err
	}
	if text.Len() == 0 {
		for _, block := range got.response.Blocks {
			if block.Type == "text" {
				text.WriteString(block.Text)
			}
		}
	}
	summary := strings.TrimSpace(text.String())
	if summary == "" {
		return "", 0, errors.New("summarizer returned no text")
	}
	return summary, got.response.OutputTokens, nil
}
