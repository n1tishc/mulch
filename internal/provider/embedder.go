package provider

import "context"

// Embedder maps text to semantic vectors. MaxBatch reports the provider's
// maximum number of texts accepted by one request.
type Embedder interface {
	Model() string
	MaxBatch() int
	Embed(context.Context, []string) ([][]float32, error)
}

// EmbeddingCache stores vectors by embedding model and content hash.
type EmbeddingCache interface {
	Load(context.Context, string, []string) (map[string][]float32, error)
	Save(context.Context, string, map[string][]float32) error
}
