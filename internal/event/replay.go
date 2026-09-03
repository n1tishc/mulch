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
		case TypeAssistantToolCall:
			var p AssistantToolCall
			if err := e.Decode(&p); err != nil {
				return nil, fmt.Errorf("decode %s: %w", e.Type, err)
			}
			appendBlock(&messages, provider.RoleAssistant, provider.Block{Type: "tool_use", CallID: p.CallID, Name: p.Name, Input: string(p.Input)})
		case TypeToolResult:
			var p ToolResult
			if err := e.Decode(&p); err != nil {
				return nil, fmt.Errorf("decode %s: %w", e.Type, err)
			}
			messages = append(messages, provider.Message{Role: provider.RoleTool, Blocks: []provider.Block{{Type: "tool_result", CallID: p.CallID, Name: p.Name, Output: p.Output, IsError: p.IsError}}})
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
