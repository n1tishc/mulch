package event

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"time"
)

func NewSessionID() (string, error) {
	var value [16]byte
	if _, err := rand.Read(value[:]); err != nil {
		return "", fmt.Errorf("create session id: %w", err)
	}
	return hex.EncodeToString(value[:]), nil
}

type Type string

const (
	TypeSessionStart      Type = "session.start"
	TypeSystemPrompt      Type = "system.prompt"
	TypeUserMessage       Type = "user.message"
	TypeLLMRequest        Type = "llm.request"
	TypeLLMResponse       Type = "llm.response"
	TypeAssistantDelta    Type = "assistant.delta"
	TypeAssistantMessage  Type = "assistant.message"
	TypeAssistantToolCall Type = "assistant.tool_call"
	TypeToolStart         Type = "tool.start"
	TypeToolResult        Type = "tool.result"
	TypeSessionEnd        Type = "session.end"
	TypeContextVisibility Type = "context.visibility"
	TypeContextInject     Type = "context.inject"
	TypeContextCompact    Type = "context.compact"
	TypeTurnCompleted     Type = "turn.completed"
	TypeScoreHealth       Type = "score.health"
	TypeScorePartial      Type = "score.partial"
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

type Node struct {
	Session  Session
	Children []Node
}

type Event struct {
	ID        int64           `json:"id"`
	SessionID string          `json:"session_id"`
	Seq       int64           `json:"seq"`
	Turn      int             `json:"turn"`
	Type      Type            `json:"type"`
	Payload   json.RawMessage `json:"payload"`
	Tokens    *int            `json:"tokens,omitempty"`
	Visible   bool            `json:"visible"`
	ImageRef  string          `json:"image_ref,omitempty"`
	CreatedAt time.Time       `json:"created_at"`
}

func (e Event) Decode(dst any) error { return json.Unmarshal(e.Payload, dst) }

type SessionStart struct {
	Task     string `json:"task"`
	Model    string `json:"model"`
	Workdir  string `json:"workdir"`
	ParentID string `json:"parent_id,omitempty"`
	ForkSeq  *int64 `json:"fork_seq,omitempty"`
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
type AssistantDelta struct {
	Text string `json:"text"`
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
type VisibilityChange struct {
	Seq  int64 `json:"seq"`
	From bool  `json:"from"`
	To   bool  `json:"to"`
}
type ContextVisibility struct {
	Changes []VisibilityChange `json:"changes"`
	Reason  string             `json:"reason"`
	By      string             `json:"by"`
}
type ContextInject struct {
	Reason string `json:"reason"`
	Text   string `json:"text"`
	By     string `json:"by"`
}
type ContextCompact struct {
	ReplacedSeqs []int64 `json:"replaced_seqs"`
	Summary      string  `json:"summary"`
	TokensBefore int     `json:"tokens_before"`
	TokensAfter  int     `json:"tokens_after"`
	By           string  `json:"by"`
}
type TurnCompleted struct{}
type ScoreHealth struct {
	TurnScored     int                       `json:"turn_scored"`
	Composite      float64                   `json:"composite"`
	Saturation     *float64                  `json:"saturation,omitempty"`
	Staleness      *float64                  `json:"staleness,omitempty"`
	Relevance      *float64                  `json:"relevance,omitempty"`
	Coherence      *float64                  `json:"coherence,omitempty"`
	Details        map[string]map[string]any `json:"details,omitempty"`
	LatencyMS      int64                     `json:"latency_ms"`
	OnCriticalPath bool                      `json:"on_critical_path"`
}
type ScorePartial struct {
	Name         string `json:"name"`
	TimeoutMS    int64  `json:"timeout_ms"`
	UsedPrevious bool   `json:"used_previous"`
}
