package event

import (
	"encoding/json"
	"time"
)

type Type string

const (
	TypeSessionStart      Type = "session.start"
	TypeSystemPrompt      Type = "system.prompt"
	TypeUserMessage       Type = "user.message"
	TypeLLMRequest        Type = "llm.request"
	TypeLLMResponse       Type = "llm.response"
	TypeAssistantMessage  Type = "assistant.message"
	TypeAssistantToolCall Type = "assistant.tool_call"
	TypeToolStart         Type = "tool.start"
	TypeToolResult        Type = "tool.result"
	TypeSessionEnd        Type = "session.end"
)

type Status string

const (
	StatusRunning   Status = "running"
	StatusCompleted Status = "completed"
	StatusFailed    Status = "failed"
	StatusCancelled Status = "cancelled"
)

type Session struct {
	ID, ParentID, Label, Task, Model, Workdir string
	ForkSeq                                   *int64
	ContextWindow                             int
	Status                                    Status
	CreatedAt                                 time.Time
	EndedAt                                   *time.Time
}

type Event struct {
	ID        int64
	SessionID string
	Seq       int64
	Turn      int
	Type      Type
	Payload   json.RawMessage
	Tokens    *int
	Visible   bool
	ImageRef  string
	CreatedAt time.Time
}

func (e Event) Decode(dst any) error { return json.Unmarshal(e.Payload, dst) }

type SessionStart struct {
	Task    string `json:"task"`
	Model   string `json:"model"`
	Workdir string `json:"workdir"`
}
type SystemPrompt struct {
	Text    string         `json:"text"`
	Sources []PromptSource `json:"sources"`
}
type PromptSource struct {
	Path       string    `json:"path"`
	ModifiedAt time.Time `json:"modified_at"`
}
type UserMessage struct {
	Text   string `json:"text"`
	Origin string `json:"origin"`
}
type LLMRequest struct {
	MessageCount     int     `json:"message_count"`
	VisibleEventSeqs []int64 `json:"visible_event_seqs"`
}
type LLMResponse struct {
	InputTokens  int    `json:"input_tokens"`
	OutputTokens int    `json:"output_tokens"`
	LatencyMS    int64  `json:"latency_ms"`
	Model        string `json:"model"`
	Cancelled    bool   `json:"cancelled"`
}
type AssistantMessage struct {
	Text       string `json:"text"`
	StopReason string `json:"stop_reason"`
}
type AssistantToolCall struct {
	CallID string          `json:"call_id"`
	Name   string          `json:"name"`
	Input  json.RawMessage `json:"input"`
}
type ToolStart struct {
	CallID    string          `json:"call_id"`
	Name      string          `json:"name"`
	Input     json.RawMessage `json:"input"`
	StartedAt time.Time       `json:"started_at"`
}
type ToolResult struct {
	CallID     string     `json:"call_id"`
	Name       string     `json:"name"`
	Output     string     `json:"output"`
	IsError    bool       `json:"is_error"`
	Cancelled  bool       `json:"cancelled"`
	TimedOut   bool       `json:"timed_out"`
	DurationMS int64      `json:"duration_ms"`
	SourceTS   *time.Time `json:"source_ts,omitempty"`
}
type SessionEnd struct {
	Status            Status `json:"status"`
	Turns             int    `json:"turns"`
	TotalInputTokens  int    `json:"total_input_tokens"`
	TotalOutputTokens int    `json:"total_output_tokens"`
	WallMS            int64  `json:"wall_ms"`
}
