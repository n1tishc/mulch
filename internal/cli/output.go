package cli

import (
	"encoding/json"
	"fmt"
	"io"

	"github.com/n1tishc/mulch/internal/event"
)

func writeJSONL(w io.Writer, events []event.Event) error {
	encoder := json.NewEncoder(w)
	encoder.SetEscapeHTML(false)
	for _, candidate := range events {
		if err := encoder.Encode(candidate); err != nil {
			return fmt.Errorf("write event %d: %w", candidate.Seq, err)
		}
	}
	return nil
}

type jsonlPublisher struct {
	writer io.Writer
	err    error
}

func (p *jsonlPublisher) Publish(candidate event.Event) {
	if p.err == nil {
		p.err = writeJSONL(p.writer, []event.Event{candidate})
	}
}

func (p *jsonlPublisher) Err() error { return p.err }

func replayTerminal(w io.Writer, events []event.Event) error {
	callOrder := make(map[int][]string)
	results := make(map[int]map[string]event.ToolResult)
	turnHasDeltas := make(map[int]bool)
	for _, candidate := range events {
		switch candidate.Type {
		case event.TypeAssistantDelta:
			turnHasDeltas[candidate.Turn] = true
		case event.TypeAssistantToolCall:
			var call event.AssistantToolCall
			if err := candidate.Decode(&call); err != nil {
				return fmt.Errorf("decode event %d: %w", candidate.Seq, err)
			}
			callOrder[candidate.Turn] = append(callOrder[candidate.Turn], call.CallID)
		case event.TypeToolResult:
			var result event.ToolResult
			if err := candidate.Decode(&result); err != nil {
				return fmt.Errorf("decode event %d: %w", candidate.Seq, err)
			}
			if results[candidate.Turn] == nil {
				results[candidate.Turn] = make(map[string]event.ToolResult)
			}
			results[candidate.Turn][result.CallID] = result
		}
	}
	flushedTools := make(map[int]bool)
	for _, candidate := range events {
		switch candidate.Type {
		case event.TypeAssistantDelta:
			var delta event.AssistantDelta
			if err := candidate.Decode(&delta); err != nil {
				return fmt.Errorf("decode event %d: %w", candidate.Seq, err)
			}
			if _, err := io.WriteString(w, delta.Text); err != nil {
				return err
			}
		case event.TypeAssistantMessage:
			if turnHasDeltas[candidate.Turn] {
				continue
			}
			var message event.AssistantMessage
			if err := candidate.Decode(&message); err != nil {
				return fmt.Errorf("decode event %d: %w", candidate.Seq, err)
			}
			if _, err := io.WriteString(w, message.Text); err != nil {
				return err
			}
		case event.TypeToolResult:
			if flushedTools[candidate.Turn] {
				continue
			}
			for _, callID := range callOrder[candidate.Turn] {
				result, ok := results[candidate.Turn][callID]
				if !ok {
					continue
				}
				if _, err := fmt.Fprintf(w, "\n[%s] %s\n", result.Name, result.Output); err != nil {
					return err
				}
			}
			flushedTools[candidate.Turn] = true
		}
	}
	return nil
}

func writeSessions(w io.Writer, sessions []event.Session) error {
	for _, session := range sessions {
		fork := "-"
		if session.ForkSeq != nil {
			fork = fmt.Sprint(*session.ForkSeq)
		}
		if _, err := fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\n", session.ID, session.Status, session.Label, session.ParentID, fork); err != nil {
			return err
		}
	}
	return nil
}

func writeTree(w io.Writer, root event.Node) error {
	var visit func(event.Node, string) error
	visit = func(node event.Node, indent string) error {
		fork := ""
		if node.Session.ForkSeq != nil {
			fork = fmt.Sprintf(" fork=%d", *node.Session.ForkSeq)
		}
		label := ""
		if node.Session.Label != "" {
			label = fmt.Sprintf(" %q", node.Session.Label)
		}
		if _, err := fmt.Fprintf(w, "%s%s [%s]%s%s\n", indent, node.Session.ID, node.Session.Status, fork, label); err != nil {
			return err
		}
		for _, child := range node.Children {
			if err := visit(child, indent+"  "); err != nil {
				return err
			}
		}
		return nil
	}
	return visit(root, "")
}
