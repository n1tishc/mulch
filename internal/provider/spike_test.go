//go:build spike

package provider_test

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/option"
)

const (
	defaultSpikeBaseURL = "https://opencode.ai/zen/go/v1"
	defaultSpikeModel   = "glm-5.3-flash"
)

func TestLiveProviderGate(t *testing.T) {
	apiKey := os.Getenv("MULCH_PROVIDER_API_KEY")
	if apiKey == "" {
		t.Skip("MULCH_PROVIDER_API_KEY is required for the live provider gate")
	}
	baseURL := os.Getenv("MULCH_PROVIDER_BASE_URL")
	if baseURL == "" {
		baseURL = defaultSpikeBaseURL
	}
	model := os.Getenv("MULCH_MODEL")
	if model == "" {
		model = defaultSpikeModel
	}
	client := openai.NewClient(option.WithAPIKey(apiKey), option.WithBaseURL(baseURL))

	t.Run("single step preserves blocks and usage without executing tools", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
		defer cancel()
		params := probeParams(model)
		stream := client.Chat.Completions.NewStreaming(ctx, params)
		acc := openai.ChatCompletionAccumulator{}
		var rawPromptTokens, rawCompletionTokens int64
		for stream.Next() {
			chunk := stream.Current()
			if chunk.Usage.TotalTokens > 0 {
				rawPromptTokens = chunk.Usage.PromptTokens
				rawCompletionTokens = chunk.Usage.CompletionTokens
			}
			if !acc.AddChunk(chunk) {
				t.Fatal("provider returned an inconsistent stream")
			}
		}
		if err := stream.Err(); err != nil {
			t.Fatal(err)
		}
		if len(acc.Choices) != 1 {
			t.Fatalf("choices = %d, want 1", len(acc.Choices))
		}
		message := acc.Choices[0].Message
		if !strings.Contains(message.Content, "probing") {
			t.Fatalf("assistant text = %q, want it to contain probing", message.Content)
		}
		if len(message.ToolCalls) != 1 {
			t.Fatalf("tool calls = %d, want 1", len(message.ToolCalls))
		}
		call := message.ToolCalls[0]
		if call.ID == "" || call.Function.Name != "record_probe" {
			t.Fatalf("tool call = %+v, want a provider ID and name record_probe", call)
		}
		if acc.Usage.PromptTokens <= 0 || acc.Usage.CompletionTokens <= 0 {
			t.Fatalf("usage = %+v, want positive provider counts", acc.Usage)
		}
		if acc.Usage.PromptTokens != rawPromptTokens || acc.Usage.CompletionTokens != rawCompletionTokens {
			t.Fatalf("accumulated usage = %d/%d, raw provider usage = %d/%d",
				acc.Usage.PromptTokens, acc.Usage.CompletionTokens, rawPromptTokens, rawCompletionTokens)
		}

		assertRoundTripAccepted(t, &client, params, message, call.ID)
	})

	t.Run("stream cancellation returns the context error promptly", func(t *testing.T) {
		ctx, cancel := context.WithCancel(t.Context())
		stream := client.Chat.Completions.NewStreaming(ctx, openai.ChatCompletionNewParams{
			Messages: []openai.ChatCompletionMessageParamUnion{
				openai.UserMessage("Write the integers from 1 through 10000, one per line."),
			},
			Model: openai.ChatModel(model),
		})
		if !stream.Next() {
			cancel()
			t.Fatalf("provider closed before cancellation: %v", stream.Err())
		}

		started := time.Now()
		cancel()
		done := make(chan error, 1)
		go func() {
			for stream.Next() {
			}
			done <- stream.Err()
		}()
		var streamErr error
		select {
		case streamErr = <-done:
		case <-time.After(500 * time.Millisecond):
			_ = stream.Close()
			t.Fatal("stream did not return within 500ms of cancellation")
		}
		elapsed := time.Since(started)
		if elapsed >= 500*time.Millisecond {
			t.Fatalf("cancellation took %s, want <500ms", elapsed)
		}
		if !errors.Is(streamErr, context.Canceled) {
			t.Fatalf("stream error = %v, want context.Canceled", streamErr)
		}
	})
}

func probeParams(model string) openai.ChatCompletionNewParams {
	return openai.ChatCompletionNewParams{
		Messages: []openai.ChatCompletionMessageParamUnion{
			openai.UserMessage("First say exactly 'probing'. Then call record_probe once with value 'mulch-provider-probe'."),
		},
		Model: openai.ChatModel(model),
		Tools: []openai.ChatCompletionToolUnionParam{
			openai.ChatCompletionFunctionTool(openai.FunctionDefinitionParam{
				Name:        "record_probe",
				Description: openai.String("Record the supplied probe value."),
				Parameters: openai.FunctionParameters{
					"type": "object",
					"properties": map[string]any{
						"value": map[string]string{"type": "string"},
					},
					"required": []string{"value"},
				},
			}),
		},
		StreamOptions: openai.ChatCompletionStreamOptionsParam{IncludeUsage: openai.Bool(true)},
	}
}

func assertRoundTripAccepted(t *testing.T, client *openai.Client, params openai.ChatCompletionNewParams, message openai.ChatCompletionMessage, callID string) {
	t.Helper()
	logged, err := json.Marshal(message)
	if err != nil {
		t.Fatalf("log assistant blocks: %v", err)
	}
	var replayed openai.ChatCompletionMessage
	if err := json.Unmarshal(logged, &replayed); err != nil {
		t.Fatalf("reconstruct assistant blocks: %v", err)
	}
	if replayed.Content != message.Content || len(replayed.ToolCalls) != 1 ||
		replayed.ToolCalls[0].ID != callID || replayed.ToolCalls[0].Function.Name != message.ToolCalls[0].Function.Name ||
		replayed.ToolCalls[0].Function.Arguments != message.ToolCalls[0].Function.Arguments {
		t.Fatalf("assistant blocks changed during log round-trip: before=%+v after=%+v", message, replayed)
	}
	params.Messages = append(params.Messages,
		replayed.ToParam(),
		openai.ToolMessage(`{"recorded":true}`, callID),
		openai.UserMessage("Reply with exactly 'round-trip-ok'."),
	)
	params.Tools = nil
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	result, err := client.Chat.Completions.New(ctx, params)
	if err != nil {
		t.Fatalf("provider rejected replayed assistant blocks: %v", err)
	}
	if len(result.Choices) != 1 || !strings.Contains(strings.ToLower(result.Choices[0].Message.Content), "round-trip-ok") {
		t.Fatalf("round-trip response = %+v, want round-trip-ok", result.Choices)
	}
}
