package provider

import "context"

type Role string

const (
	RoleSystem    Role = "system"
	RoleUser      Role = "user"
	RoleAssistant Role = "assistant"
	RoleTool      Role = "tool"
)

type Block struct {
	Type    string `json:"type"`
	Text    string `json:"text,omitempty"`
	CallID  string `json:"call_id,omitempty"`
	Name    string `json:"name,omitempty"`
	Input   string `json:"input,omitempty"`
	Output  string `json:"output,omitempty"`
	IsError bool   `json:"is_error,omitempty"`
}

type Message struct {
	Role   Role    `json:"role"`
	Blocks []Block `json:"blocks"`
}

type Request struct {
	Messages  []Message
	Tools     []ToolSpec
	MaxTokens int
	Model     string
}

type ToolSpec struct {
	Name, Description string
	InputSchema       map[string]any
}

type Delta struct{ Text string }

type Response struct {
	Blocks                    []Block
	StopReason, Model         string
	InputTokens, OutputTokens int
}

type LLM interface {
	Stream(context.Context, Request, chan<- Delta) (Response, error)
}
