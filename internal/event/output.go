package event

import (
	"encoding/json"
	"fmt"
	"io"
)

// WriteJSONL writes complete committed events for process integrations.
func WriteJSONL(w io.Writer, events []Event) error {
	encoder := json.NewEncoder(w)
	encoder.SetEscapeHTML(false)
	for _, e := range events {
		if err := encoder.Encode(e); err != nil {
			return fmt.Errorf("write event %d: %w", e.Seq, err)
		}
	}
	return nil
}

// JSONLPublisher writes an event only after the store has committed it.
type JSONLPublisher struct {
	Writer io.Writer
	err    error
}

func (p *JSONLPublisher) Publish(e Event) {
	if p.err != nil {
		return
	}
	p.err = WriteJSONL(p.Writer, []Event{e})
}

func (p *JSONLPublisher) Err() error { return p.err }

// ReplayTerminal renders durable streaming deltas and compact tool results.
func ReplayTerminal(w io.Writer, events []Event) error {
	callOrder := make(map[int][]string)
	results := make(map[int]map[string]ToolResult)
	for _, e := range events {
		switch e.Type {
		case TypeAssistantToolCall:
			var call AssistantToolCall
			if err := e.Decode(&call); err != nil {
				return fmt.Errorf("decode event %d: %w", e.Seq, err)
			}
			callOrder[e.Turn] = append(callOrder[e.Turn], call.CallID)
		case TypeToolResult:
			var result ToolResult
			if err := e.Decode(&result); err != nil {
				return fmt.Errorf("decode event %d: %w", e.Seq, err)
			}
			if results[e.Turn] == nil {
				results[e.Turn] = make(map[string]ToolResult)
			}
			results[e.Turn][result.CallID] = result
		}
	}
	flushedTools := make(map[int]bool)
	for _, e := range events {
		switch e.Type {
		case TypeAssistantDelta:
			var delta AssistantDelta
			if err := e.Decode(&delta); err != nil {
				return fmt.Errorf("decode event %d: %w", e.Seq, err)
			}
			if _, err := io.WriteString(w, delta.Text); err != nil {
				return err
			}
		case TypeToolResult:
			if flushedTools[e.Turn] {
				continue
			}
			for _, callID := range callOrder[e.Turn] {
				result, ok := results[e.Turn][callID]
				if !ok {
					continue
				}
				if _, err := fmt.Fprintf(w, "\n[%s] %s\n", result.Name, result.Output); err != nil {
					return err
				}
			}
			flushedTools[e.Turn] = true
		}
	}
	return nil
}
