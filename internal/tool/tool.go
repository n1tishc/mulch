package tool

import (
	"context"
	"encoding/json"
	"time"
)

type Result struct {
	Output   string
	IsError  bool
	SourceTS time.Time
}

type Tool interface {
	Name() string
	Description() string
	InputSchema() map[string]any
	Run(context.Context, json.RawMessage) (Result, error)
}

func invalidInput(err error) (Result, error) {
	return Result{Output: "invalid input: " + err.Error(), IsError: true}, nil
}

func schema(properties map[string]any, required ...string) map[string]any {
	return map[string]any{"type": "object", "properties": properties, "required": required, "additionalProperties": false}
}
