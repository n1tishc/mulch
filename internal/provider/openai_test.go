package provider_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"github.com/n1tishc/mulch/internal/provider"
)

func TestOpenAIProductionStreamPreservesSessionBlocksUsageAndReplay(t *testing.T) {
	var requests []map[string]any
	endpoint := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/chat/completions" || r.Header.Get("x-opencode-session") != "release-test" || r.Header.Get("Authorization") != "Bearer fixture-key" {
			t.Error("wrong production request path, session identity, or authentication")
		}
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
			return
		}
		requests = append(requests, body)
		w.Header().Set("Content-Type", "text/event-stream")
		if len(requests) == 1 {
			for _, delta := range []string{
				`{"role":"assistant","content":"probing"}`,
				`{"tool_calls":[{"index":0,"id":"call-1","type":"function","function":{"name":"record_probe","arguments":"{\"value\":"}}]}`,
				`{"tool_calls":[{"index":0,"function":{"arguments":"\"fixture\"}"}}]}`,
			} {
				_, _ = fmt.Fprintf(w, "data: {\"id\":\"reply\",\"model\":\"actual-model\",\"choices\":[{\"index\":0,\"delta\":%s}]}\n\n", delta)
			}
			_, _ = fmt.Fprint(w, "data: {\"id\":\"reply\",\"model\":\"actual-model\",\"choices\":[{\"index\":0,\"delta\":{},\"finish_reason\":\"tool_calls\"}]}\n\n")
		} else {
			_, _ = fmt.Fprint(w, "data: {\"id\":\"reply\",\"model\":\"actual-model\",\"choices\":[{\"index\":0,\"delta\":{\"role\":\"assistant\",\"content\":\"round-trip-ok\"},\"finish_reason\":\"stop\"}]}\n\n")
		}
		_, _ = fmt.Fprint(w, "data: {\"id\":\"reply\",\"model\":\"actual-model\",\"choices\":[],\"usage\":{\"prompt_tokens\":17,\"completion_tokens\":9,\"total_tokens\":26}}\n\ndata: [DONE]\n\n")
	}))
	defer endpoint.Close()
	llm := provider.NewOpenAI("fixture-key", endpoint.URL)
	req := provider.Request{SessionID: "release-test", Model: "requested-model", NoRetry: true,
		Messages: []provider.Message{{Role: provider.RoleUser, Blocks: []provider.Block{{Type: "text", Text: "probe"}}}},
		Tools:    []provider.ToolSpec{{Name: "record_probe", InputSchema: map[string]any{"type": "object"}}},
	}
	run := func() (provider.Response, string) {
		t.Helper()
		ctx, cancel := context.WithCancel(t.Context())
		defer cancel()
		out := make(chan provider.Delta)
		done := make(chan string, 1)
		go func() {
			var text strings.Builder
			for delta := range out {
				text.WriteString(delta.Text)
			}
			done <- text.String()
		}()
		response, err := llm.Stream(ctx, req, out)
		close(out)
		text := <-done
		if err != nil {
			t.Fatal(err)
		}
		return response, text
	}
	response, text := run()
	want := []provider.Block{{Type: "text", Text: "probing"}, {Type: "tool_use", CallID: "call-1", Name: "record_probe", Input: `{"value":"fixture"}`}}
	if !reflect.DeepEqual(response.Blocks, want) || text != "probing" || response.InputTokens != 17 || response.OutputTokens != 9 || response.Model != "actual-model" || response.StopReason != "tool_calls" {
		t.Fatalf("stream translation = %+v, text=%q", response, text)
	}
	req.Messages = append(req.Messages,
		provider.Message{Role: provider.RoleAssistant, Blocks: response.Blocks},
		provider.Message{Role: provider.RoleTool, Blocks: []provider.Block{{Type: "tool_result", CallID: "call-1", Name: "record_probe", Output: "recorded"}}},
	)
	req.Tools = nil
	_, text = run()
	if text != "round-trip-ok" {
		t.Fatal("missing follow-up")
	}
	messages := requests[1]["messages"].([]any)
	assistant := messages[1].(map[string]any)
	call := assistant["tool_calls"].([]any)[0].(map[string]any)
	result := messages[2].(map[string]any)
	if assistant["content"] != "probing" || call["id"] != "call-1" || call["function"].(map[string]any)["arguments"] != `{"value":"fixture"}` || result["tool_call_id"] != "call-1" || result["content"] != "recorded" {
		t.Fatalf("replayed wire messages = %+v", messages)
	}
}
