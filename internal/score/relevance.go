package score

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"math"
	"sort"
	"time"

	"github.com/n1tishc/mulch/internal/event"
	"github.com/n1tishc/mulch/internal/provider"
)

const relevanceChunkBytes = 2000

type Relevance struct {
	embedder provider.Embedder
	cache    provider.EmbeddingCache
}

type RelevanceDetail struct {
	Seq        int64   `json:"seq"`
	Similarity float64 `json:"sim"`
}

func NewRelevance(embedder provider.Embedder, cache provider.EmbeddingCache) Relevance {
	return Relevance{embedder: embedder, cache: cache}
}

func (Relevance) Name() string            { return "relevance" }
func (Relevance) Deadline() time.Duration { return 3 * time.Second }

type relevanceChunk struct {
	seq  int64
	turn int
	text string
}

func (r Relevance) Score(ctx context.Context, input Input) (Result, error) {
	if r.embedder == nil || r.cache == nil {
		return Result{}, errors.New("relevance embedder is not configured")
	}
	chunks := relevanceChunks(input.Visible)
	if len(chunks) == 0 || input.Session.Task == "" {
		return Result{Score: 1, Details: map[string]any{"lowest": []RelevanceDetail{}}}, nil
	}
	texts := make([]string, 1, len(chunks)+1)
	texts[0] = input.Session.Task
	for _, chunk := range chunks {
		texts = append(texts, chunk.text)
	}
	vectors, err := r.vectors(ctx, texts)
	if err != nil {
		return Result{}, err
	}
	task := vectors[contentHash(input.Session.Task)]
	var weighted, weights float64
	bySeq := map[int64][]float64{}
	for _, chunk := range chunks {
		sim := cosine(task, vectors[contentHash(chunk.text)])
		age := input.Turn - chunk.turn
		if age < 0 {
			age = 0
		}
		weight := math.Exp(-float64(age) / 8)
		weighted += sim * weight
		weights += weight
		bySeq[chunk.seq] = append(bySeq[chunk.seq], sim)
	}
	mean := weighted / weights
	score := clamp((mean-.2)/.7, 0, 1)
	lowest := make([]RelevanceDetail, 0, len(bySeq))
	for seq, similarities := range bySeq {
		var total float64
		for _, similarity := range similarities {
			total += similarity
		}
		lowest = append(lowest, RelevanceDetail{Seq: seq, Similarity: total / float64(len(similarities))})
	}
	sort.Slice(lowest, func(i, j int) bool {
		if lowest[i].Similarity == lowest[j].Similarity {
			return lowest[i].Seq < lowest[j].Seq
		}
		return lowest[i].Similarity < lowest[j].Similarity
	})
	if len(lowest) > 5 {
		lowest = lowest[:5]
	}
	return Result{Score: score, Details: map[string]any{"lowest": lowest}}, nil
}

func (r Relevance) vectors(ctx context.Context, texts []string) (map[string][]float32, error) {
	hashes := make([]string, len(texts))
	for i, text := range texts {
		hashes[i] = contentHash(text)
	}
	found, err := r.cache.Load(ctx, r.embedder.Model(), hashes)
	if err != nil {
		return nil, err
	}
	missingTexts := make([]string, 0, len(texts))
	missingHashes := make([]string, 0, len(texts))
	seen := map[string]bool{}
	for i, hash := range hashes {
		if _, ok := found[hash]; ok || seen[hash] {
			continue
		}
		seen[hash] = true
		missingHashes = append(missingHashes, hash)
		missingTexts = append(missingTexts, texts[i])
	}
	batchSize := r.embedder.MaxBatch()
	if batchSize <= 0 {
		batchSize = len(missingTexts)
	}
	created := map[string][]float32{}
	for start := 0; start < len(missingTexts); start += batchSize {
		end := min(start+batchSize, len(missingTexts))
		batch, embedErr := r.embedder.Embed(ctx, missingTexts[start:end])
		if embedErr != nil {
			return nil, embedErr
		}
		if len(batch) != end-start {
			return nil, errors.New("embedding provider returned wrong vector count")
		}
		for i, vector := range batch {
			created[missingHashes[start+i]] = vector
			found[missingHashes[start+i]] = vector
		}
	}
	if len(created) > 0 {
		if err := r.cache.Save(ctx, r.embedder.Model(), created); err != nil {
			return nil, err
		}
	}
	return found, nil
}

func relevanceChunks(events []event.Event) []relevanceChunk {
	var result []relevanceChunk
	for _, candidate := range events {
		var text string
		switch candidate.Type {
		case event.TypeAssistantMessage:
			var value event.AssistantMessage
			if candidate.Decode(&value) == nil {
				text = value.Text
			}
		case event.TypeToolResult:
			var value event.ToolResult
			if candidate.Decode(&value) == nil {
				text = value.Output
			}
		case event.TypeContextInject:
			var value event.ContextInject
			if candidate.Decode(&value) == nil {
				text = value.Text
			}
		case event.TypeContextCompact:
			var value event.ContextCompact
			if candidate.Decode(&value) == nil {
				text = value.Summary
			}
		}
		for len(text) > 0 {
			end := min(len(text), relevanceChunkBytes)
			for end < len(text) && end > 0 && text[end]&0xc0 == 0x80 {
				end--
			}
			if end == 0 {
				end = min(len(text), relevanceChunkBytes)
			}
			result = append(result, relevanceChunk{seq: candidate.Seq, turn: candidate.Turn, text: text[:end]})
			text = text[end:]
		}
	}
	return result
}

func contentHash(text string) string {
	sum := sha256.Sum256([]byte(text))
	return hex.EncodeToString(sum[:])
}

func cosine(a, b []float32) float64 {
	if len(a) == 0 || len(a) != len(b) {
		return 0
	}
	var dot, aa, bb float64
	for i := range a {
		x, y := float64(a[i]), float64(b[i])
		dot += x * y
		aa += x * x
		bb += y * y
	}
	if aa == 0 || bb == 0 {
		return 0
	}
	return dot / math.Sqrt(aa*bb)
}

func clamp(value, low, high float64) float64 {
	return math.Max(low, math.Min(high, value))
}
