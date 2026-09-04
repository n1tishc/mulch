package event_test

import (
	"encoding/json"
	"reflect"
	"testing"

	"github.com/n1tishc/mulch/internal/event"
	"github.com/n1tishc/mulch/internal/provider"
)

func TestBuildMessagesReconstructsProviderInput(t *testing.T) {
	events := []event.Event{
		{Seq: 1, Type: event.TypeSystemPrompt, Visible: true, Payload: payload(t, event.SystemPrompt{Text: "system"})},
		{Seq: 2, Type: event.TypeUserMessage, Visible: true, Payload: payload(t, event.UserMessage{Text: "hello", Origin: "task"})},
		{Seq: 3, Type: event.TypeLLMRequest, Visible: true, Payload: payload(t, event.LLMRequest{VisibleEventSeqs: []int64{1, 2}})},
	}
	want := []provider.Message{
		{Role: provider.RoleSystem, Blocks: []provider.Block{{Type: "text", Text: "system"}}},
		{Role: provider.RoleUser, Blocks: []provider.Block{{Type: "text", Text: "hello"}}},
	}
	got, err := event.BuildMessages(events, []int64{1, 2})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("messages = %#v, want %#v", got, want)
	}
}

func TestBuildMessagesIncludesInterventionContext(t *testing.T) {
	events := []event.Event{
		{Seq: 1, Type: event.TypeContextInject, Payload: payload(t, event.ContextInject{Text: "recheck the task"})},
		{Seq: 2, Type: event.TypeContextCompact, Payload: payload(t, event.ContextCompact{Summary: "Paths: main.go. Decision: keep SQLite. Open question: timeout?"})},
	}
	want := []provider.Message{
		{Role: provider.RoleUser, Blocks: []provider.Block{{Type: "text", Text: "recheck the task"}}},
		{Role: provider.RoleUser, Blocks: []provider.Block{{Type: "text", Text: "Compacted context:\nPaths: main.go. Decision: keep SQLite. Open question: timeout?"}}},
	}
	got, err := event.BuildMessages(events, []int64{1, 2})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("messages = %#v, want %#v", got, want)
	}
}

func TestPayloadsUseCanonicalJSONKeys(t *testing.T) {
	got := string(payload(t, event.SessionEnd{Status: event.StatusCompleted, Turns: 1, TotalInputTokens: 2, TotalOutputTokens: 3, WallMS: 4}))
	want := `{"status":"completed","turns":1,"total_input_tokens":2,"total_output_tokens":3,"wall_ms":4}`
	if got != want {
		t.Fatalf("payload = %s, want %s", got, want)
	}
}

func TestBuildMessagesReconstructsToolTurn(t *testing.T) {
	events := []event.Event{
		{Seq: 1, Type: event.TypeAssistantMessage, Payload: payload(t, event.AssistantMessage{Text: "checking"})},
		{Seq: 2, Type: event.TypeAssistantToolCall, Payload: payload(t, event.AssistantToolCall{CallID: "call-1", Name: "read", Input: json.RawMessage(`{"path":"hello.go"}`)})},
		{Seq: 3, Type: event.TypeToolResult, Payload: payload(t, event.ToolResult{CallID: "call-1", Name: "read", Output: "package main", DurationMS: 2})},
	}
	want := []provider.Message{
		{Role: provider.RoleAssistant, Blocks: []provider.Block{{Type: "text", Text: "checking"}, {Type: "tool_use", CallID: "call-1", Name: "read", Input: `{"path":"hello.go"}`}}},
		{Role: provider.RoleTool, Blocks: []provider.Block{{Type: "tool_result", CallID: "call-1", Name: "read", Output: "package main"}}},
	}
	got, err := event.BuildMessages(events, []int64{1, 2, 3})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("messages = %#v, want %#v", got, want)
	}
}

func TestBuildMessagesKeepsToolResultsInOriginalCallOrder(t *testing.T) {
	events := []event.Event{
		{Seq: 1, Turn: 1, Type: event.TypeAssistantToolCall, Payload: payload(t, event.AssistantToolCall{CallID: "slow", Name: "read", Input: json.RawMessage(`{"path":"slow"}`)})},
		{Seq: 2, Turn: 1, Type: event.TypeAssistantToolCall, Payload: payload(t, event.AssistantToolCall{CallID: "fast", Name: "read", Input: json.RawMessage(`{"path":"fast"}`)})},
		// Completion events may be persisted in completion order.
		{Seq: 3, Turn: 1, Type: event.TypeToolResult, Payload: payload(t, event.ToolResult{CallID: "fast", Name: "read", Output: "second"})},
		{Seq: 4, Turn: 1, Type: event.TypeToolResult, Payload: payload(t, event.ToolResult{CallID: "slow", Name: "read", Output: "first"})},
	}
	want := []provider.Message{
		{Role: provider.RoleAssistant, Blocks: []provider.Block{
			{Type: "tool_use", CallID: "slow", Name: "read", Input: `{"path":"slow"}`},
			{Type: "tool_use", CallID: "fast", Name: "read", Input: `{"path":"fast"}`},
		}},
		{Role: provider.RoleTool, Blocks: []provider.Block{{Type: "tool_result", CallID: "slow", Name: "read", Output: "first"}}},
		{Role: provider.RoleTool, Blocks: []provider.Block{{Type: "tool_result", CallID: "fast", Name: "read", Output: "second"}}},
	}
	got, err := event.BuildMessages(events, []int64{1, 2, 3, 4})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("messages = %#v, want %#v", got, want)
	}
}

func payload(t *testing.T, v any) []byte {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return b
}
