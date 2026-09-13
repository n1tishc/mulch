//go:build spike

package provider_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/n1tishc/mulch/internal/provider"
)

// Opt-in: this test sends real requests that can consume provider credits.
func TestLiveProviderGate(t *testing.T) {
	apiKey := os.Getenv("MULCH_PROVIDER_API_KEY")
	if apiKey == "" {
		t.Skip("MULCH_PROVIDER_API_KEY is required; live requests may consume credits")
	}
	baseURL := os.Getenv("MULCH_PROVIDER_BASE_URL")
	model := os.Getenv("MULCH_MODEL")
	if model == "" {
		model = "glm-5.3-flash"
	}
	llm := provider.NewOpenAI(apiKey, baseURL)
	sessionID := fmt.Sprintf("mulch-release-probe-%d", time.Now().UnixNano())
	safeError := func(err error) string {
		if err == nil {
			return "<nil>"
		}
		return strings.ReplaceAll(err.Error(), apiKey, "[redacted]")
	}
	stream := func(t *testing.T, req provider.Request) (provider.Response, string) {
		t.Helper()
		ctx, cancel := context.WithTimeout(t.Context(), 60*time.Second)
		defer cancel()
		deltas := make(chan provider.Delta)
		text := make(chan string, 1)
		go func() {
			var output strings.Builder
			for delta := range deltas {
				output.WriteString(delta.Text)
			}
			text <- output.String()
		}()
		response, err := llm.Stream(ctx, req, deltas)
		close(deltas)
		output := <-text
		if err != nil {
			t.Fatalf("production adapter: %s", safeError(err))
		}
		return response, output
	}
	t.Run("text tool usage and same-session replay", func(t *testing.T) {
		req := provider.Request{
			SessionID: sessionID, Model: model, NoRetry: true, MaxTokens: 1024,
			Messages: []provider.Message{{Role: provider.RoleUser, Blocks: []provider.Block{{Type: "text", Text: "First say exactly 'probing'. Then call record_probe once with value 'mulch-provider-probe'."}}}},
			Tools: []provider.ToolSpec{{Name: "record_probe", Description: "Record the supplied probe value.", InputSchema: map[string]any{
				"type": "object", "properties": map[string]any{"value": map[string]string{"type": "string"}}, "required": []string{"value"},
			}}},
		}
		response, streamed := stream(t, req)
		if !strings.Contains(streamed, "probing") {
			t.Fatal("missing streamed probe text")
		}
		if response.InputTokens <= 0 || response.OutputTokens <= 0 {
			t.Fatalf("missing provider usage: %d/%d", response.InputTokens, response.OutputTokens)
		}
		var calls []provider.Block
		var finalText strings.Builder
		for _, block := range response.Blocks {
			if block.Type == "tool_use" {
				calls = append(calls, block)
			}
			if block.Type == "text" {
				finalText.WriteString(block.Text)
			}
		}
		if streamed != finalText.String() {
			t.Fatal("streamed text differs from persisted blocks")
		}
		if len(calls) != 1 || calls[0].CallID == "" || calls[0].Name != "record_probe" {
			t.Fatal("expected one named tool call with a provider-assigned ID")
		}
		var arguments struct {
			Value string `json:"value"`
		}
		if err := json.Unmarshal([]byte(calls[0].Input), &arguments); err != nil || arguments.Value != "mulch-provider-probe" {
			t.Fatal("incorrect tool arguments")
		}
		assistant := provider.Message{Role: provider.RoleAssistant, Blocks: response.Blocks}
		logged, err := json.Marshal(assistant)
		if err != nil {
			t.Fatal(err)
		}
		var replayed provider.Message
		if err = json.Unmarshal(logged, &replayed); err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(assistant, replayed) {
			t.Fatal("canonical message changed in JSON round-trip")
		}
		req.Messages = append(req.Messages, replayed,
			provider.Message{Role: provider.RoleTool, Blocks: []provider.Block{{Type: "tool_result", CallID: calls[0].CallID, Name: calls[0].Name, Output: `{"recorded":true}`}}},
			provider.Message{Role: provider.RoleUser, Blocks: []provider.Block{{Type: "text", Text: "Reply with exactly 'round-trip-ok'."}}},
		)
		req.Tools = nil
		_, followup := stream(t, req)
		if !strings.Contains(followup, "round-trip-ok") {
			t.Fatal("same-session follow-up missing expected text")
		}
	})
	t.Run("stream cancellation", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(t.Context(), 60*time.Second)
		defer cancel()
		deltas := make(chan provider.Delta)
		done := make(chan error, 1)
		go func() {
			_, err := llm.Stream(ctx, provider.Request{
				SessionID: sessionID, Model: model, NoRetry: true, MaxTokens: 1024,
				Messages: []provider.Message{{Role: provider.RoleUser, Blocks: []provider.Block{{Type: "text", Text: "Write the integers from 1 through 10000, one per line."}}}},
			}, deltas)
			done <- err
		}()
		select {
		case <-deltas:
		case err := <-done:
			t.Fatalf("stream ended before first text: %s", safeError(err))
		case <-ctx.Done():
			t.Fatal("provider did not stream text before deadline")
		}
		cancel()
		select {
		case err := <-done:
			if !errors.Is(err, context.Canceled) {
				t.Fatalf("expected context.Canceled: %s", safeError(err))
			}
		case <-time.After(2 * time.Second):
			t.Fatal("production adapter did not stop within cancellation watchdog")
		}
	})
}
