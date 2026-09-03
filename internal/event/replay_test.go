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

func TestPayloadsUseCanonicalJSONKeys(t *testing.T) {
	got := string(payload(t, event.SessionEnd{Status: event.StatusCompleted, Turns: 1, TotalInputTokens: 2, TotalOutputTokens: 3, WallMS: 4}))
	want := `{"status":"completed","turns":1,"total_input_tokens":2,"total_output_tokens":3,"wall_ms":4}`
	if got != want {
		t.Fatalf("payload = %s, want %s", got, want)
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
