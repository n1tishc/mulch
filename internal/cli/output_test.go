package cli

import (
	"bytes"
	"encoding/json"
	"testing"

	"github.com/n1tishc/mulch/internal/event"
)

func TestReplayTerminalPresentsParallelToolsInModelCallOrder(t *testing.T) {
	events := []event.Event{
		{Seq: 1, Turn: 1, Type: event.TypeAssistantToolCall, Payload: outputPayload(t, event.AssistantToolCall{CallID: "slow", Name: "read"})},
		{Seq: 2, Turn: 1, Type: event.TypeAssistantToolCall, Payload: outputPayload(t, event.AssistantToolCall{CallID: "fast", Name: "bash"})},
		{Seq: 3, Turn: 1, Type: event.TypeToolResult, Payload: outputPayload(t, event.ToolResult{CallID: "fast", Name: "bash", Output: "second"})},
		{Seq: 4, Turn: 1, Type: event.TypeToolResult, Payload: outputPayload(t, event.ToolResult{CallID: "slow", Name: "read", Output: "first"})},
	}
	var got bytes.Buffer
	if err := replayTerminal(&got, events); err != nil {
		t.Fatal(err)
	}
	if want := "\n[read] first\n\n[bash] second\n"; got.String() != want {
		t.Fatalf("output = %q, want %q", got.String(), want)
	}
}

func TestReplayTerminalFallsBackToRecordedAssistantMessage(t *testing.T) {
	events := []event.Event{{Seq: 1, Turn: 1, Type: event.TypeAssistantMessage, Payload: outputPayload(t, event.AssistantMessage{Text: "recorded answer"})}}
	var got bytes.Buffer
	if err := replayTerminal(&got, events); err != nil {
		t.Fatal(err)
	}
	if got.String() != "recorded answer" {
		t.Fatalf("output = %q", got.String())
	}
}

func outputPayload(t *testing.T, value any) json.RawMessage {
	t.Helper()
	b, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return b
}
