package event

import (
	"fmt"

	"github.com/n1tishc/mulch/internal/provider"
)

func BuildMessages(events []Event, visibleSeqs []int64) ([]provider.Message, error) {
	visible := make(map[int64]struct{}, len(visibleSeqs))
	for _, seq := range visibleSeqs {
		visible[seq] = struct{}{}
	}
	resultsByTurn := make(map[int]map[string]ToolResult)
	for _, e := range events {
		if _, ok := visible[e.Seq]; !ok || e.Type != TypeToolResult {
			continue
		}
		var result ToolResult
		if err := e.Decode(&result); err != nil {
			return nil, fmt.Errorf("decode %s: %w", e.Type, err)
		}
		if resultsByTurn[e.Turn] == nil {
			resultsByTurn[e.Turn] = make(map[string]ToolResult)
		}
		resultsByTurn[e.Turn][result.CallID] = result
	}
	callOrderByTurn := make(map[int][]string)
	flushedResults := make(map[int]bool)
	var messages []provider.Message
	for _, e := range events {
		if _, ok := visible[e.Seq]; !ok {
			continue
		}
		switch e.Type {
		case TypeSystemPrompt:
			var p SystemPrompt
			if err := e.Decode(&p); err != nil {
				return nil, fmt.Errorf("decode %s: %w", e.Type, err)
			}
			messages = append(messages, provider.Message{Role: provider.RoleSystem, Blocks: []provider.Block{{Type: "text", Text: p.Text}}})
		case TypeUserMessage:
			var p UserMessage
			if err := e.Decode(&p); err != nil {
				return nil, fmt.Errorf("decode %s: %w", e.Type, err)
			}
			messages = append(messages, provider.Message{Role: provider.RoleUser, Blocks: []provider.Block{{Type: "text", Text: p.Text}}})
		case TypeAssistantMessage:
			var p AssistantMessage
			if err := e.Decode(&p); err != nil {
				return nil, fmt.Errorf("decode %s: %w", e.Type, err)
			}
			messages = append(messages, provider.Message{Role: provider.RoleAssistant, Blocks: []provider.Block{{Type: "text", Text: p.Text}}})
		case TypeContextInject:
			var p ContextInject
			if err := e.Decode(&p); err != nil {
				return nil, fmt.Errorf("decode %s: %w", e.Type, err)
			}
			messages = append(messages, provider.Message{Role: provider.RoleUser, Blocks: []provider.Block{{Type: "text", Text: p.Text}}})
		case TypeContextCompact:
			var p ContextCompact
			if err := e.Decode(&p); err != nil {
				return nil, fmt.Errorf("decode %s: %w", e.Type, err)
			}
			messages = append(messages, provider.Message{Role: provider.RoleUser, Blocks: []provider.Block{{Type: "text", Text: "Compacted context:\n" + p.Summary}}})
		case TypeAssistantToolCall:
			var p AssistantToolCall
			if err := e.Decode(&p); err != nil {
				return nil, fmt.Errorf("decode %s: %w", e.Type, err)
			}
			appendBlock(&messages, provider.RoleAssistant, provider.Block{Type: "tool_use", CallID: p.CallID, Name: p.Name, Input: string(p.Input)})
			callOrderByTurn[e.Turn] = append(callOrderByTurn[e.Turn], p.CallID)
		case TypeToolResult:
			if flushedResults[e.Turn] {
				continue
			}
			for _, callID := range callOrderByTurn[e.Turn] {
				if p, ok := resultsByTurn[e.Turn][callID]; ok {
					messages = append(messages, provider.Message{Role: provider.RoleTool, Blocks: []provider.Block{{Type: "tool_result", CallID: p.CallID, Name: p.Name, Output: p.Output, IsError: p.IsError}}})
				}
			}
			flushedResults[e.Turn] = true
		}
	}
	return messages, nil
}

func appendBlock(messages *[]provider.Message, role provider.Role, block provider.Block) {
	if len(*messages) > 0 && (*messages)[len(*messages)-1].Role == role {
		last := &(*messages)[len(*messages)-1]
		last.Blocks = append(last.Blocks, block)
		return
	}
	*messages = append(*messages, provider.Message{Role: role, Blocks: []provider.Block{block}})
}
