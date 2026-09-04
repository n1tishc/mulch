package provider

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

const defaultVoyageURL = "https://api.voyageai.com"

type Voyage struct {
	apiKey  string
	baseURL string
	model   string
	client  *http.Client
}

func NewVoyage(apiKey, baseURL, model string) *Voyage {
	if baseURL == "" {
		baseURL = defaultVoyageURL
	}
	if model == "" {
		model = "voyage-4-lite"
	}
	return &Voyage{apiKey: apiKey, baseURL: strings.TrimRight(baseURL, "/"), model: model, client: http.DefaultClient}
}

func (v *Voyage) Model() string { return v.model }
func (*Voyage) MaxBatch() int   { return 1000 }

func (v *Voyage) Embed(ctx context.Context, texts []string) ([][]float32, error) {
	body, err := json.Marshal(struct {
		Input []string `json:"input"`
		Model string   `json:"model"`
	}{Input: texts, Model: v.model})
	if err != nil {
		return nil, err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, v.baseURL+"/v1/embeddings", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	request.Header.Set("Authorization", "Bearer "+v.apiKey)
	request.Header.Set("Content-Type", "application/json")
	response, err := v.client.Do(request)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		message, _ := io.ReadAll(io.LimitReader(response.Body, 4096))
		return nil, fmt.Errorf("voyage embeddings: %s: %s", response.Status, strings.TrimSpace(string(message)))
	}
	var decoded struct {
		Data []struct {
			Index     int       `json:"index"`
			Embedding []float32 `json:"embedding"`
		} `json:"data"`
	}
	if err := json.NewDecoder(response.Body).Decode(&decoded); err != nil {
		return nil, err
	}
	result := make([][]float32, len(texts))
	for _, item := range decoded.Data {
		if item.Index < 0 || item.Index >= len(result) {
			return nil, fmt.Errorf("voyage embeddings: invalid index %d", item.Index)
		}
		result[item.Index] = item.Embedding
	}
	for i, vector := range result {
		if len(vector) == 0 {
			return nil, fmt.Errorf("voyage embeddings: missing vector %d", i)
		}
	}
	return result, nil
}
