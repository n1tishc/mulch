package provider

import (
	"context"
	"fmt"

	"github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/option"
)

const DefaultBaseURL = "https://opencode.ai/zen/go/v1"

type OpenAI struct{ client openai.Client }

func NewOpenAI(apiKey, baseURL string) *OpenAI {
	if baseURL == "" {
		baseURL = DefaultBaseURL
	}
	client := openai.NewClient(option.WithAPIKey(apiKey), option.WithBaseURL(baseURL))
	return &OpenAI{client: client}
}

func (o *OpenAI) Stream(ctx context.Context, req Request, out chan<- Delta) (Response, error) {
	messages := make([]openai.ChatCompletionMessageParamUnion, 0, len(req.Messages))
	for _, message := range req.Messages {
		text := ""
		for _, block := range message.Blocks {
			if block.Type == "text" {
				text += block.Text
			}
		}
		switch message.Role {
		case RoleSystem:
			messages = append(messages, openai.SystemMessage(text))
		case RoleUser:
			messages = append(messages, openai.UserMessage(text))
		case RoleAssistant:
			messages = append(messages, openai.AssistantMessage(text))
		default:
			return Response{}, fmt.Errorf("unsupported message role %q", message.Role)
		}
	}
	params := openai.ChatCompletionNewParams{Messages: messages, Model: openai.ChatModel(req.Model), StreamOptions: openai.ChatCompletionStreamOptionsParam{IncludeUsage: openai.Bool(true)}}
	if req.MaxTokens > 0 {
		params.MaxCompletionTokens = openai.Int(int64(req.MaxTokens))
	}
	stream := o.client.Chat.Completions.NewStreaming(ctx, params)
	defer stream.Close()
	acc := openai.ChatCompletionAccumulator{}
	for stream.Next() {
		chunk := stream.Current()
		if !acc.AddChunk(chunk) {
			return Response{}, errorsNewInconsistentStream
		}
		for _, choice := range chunk.Choices {
			if choice.Delta.Content != "" {
				select {
				case out <- Delta{Text: choice.Delta.Content}:
				case <-ctx.Done():
					return Response{}, ctx.Err()
				}
			}
		}
	}
	if err := stream.Err(); err != nil {
		return Response{}, err
	}
	if len(acc.Choices) != 1 {
		return Response{}, fmt.Errorf("provider returned %d choices, want 1", len(acc.Choices))
	}
	message := acc.Choices[0].Message
	blocks := make([]Block, 0, 1+len(message.ToolCalls))
	if message.Content != "" {
		blocks = append(blocks, Block{Type: "text", Text: message.Content})
	}
	for _, call := range message.ToolCalls {
		blocks = append(blocks, Block{Type: "tool_use", CallID: call.ID, Name: call.Function.Name, Input: call.Function.Arguments})
	}
	return Response{Blocks: blocks, StopReason: string(acc.Choices[0].FinishReason), InputTokens: int(acc.Usage.PromptTokens), OutputTokens: int(acc.Usage.CompletionTokens), Model: acc.Model}, nil
}

var errorsNewInconsistentStream = fmt.Errorf("provider returned an inconsistent stream")
