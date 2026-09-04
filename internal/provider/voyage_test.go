package provider

import (
	"encoding/json"
	"io"
	"net/http"
	"reflect"
	"strings"
	"testing"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) { return f(request) }

func TestVoyageBatchesEmbeddingRequestAndOrdersVectors(t *testing.T) {
	embedder := NewVoyage("secret", "https://embedding.test", "voyage-4-lite")
	embedder.client = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.URL.Path != "/v1/embeddings" || r.Header.Get("Authorization") != "Bearer secret" {
			t.Fatalf("request = %s auth=%q", r.URL.Path, r.Header.Get("Authorization"))
		}
		var body struct {
			Input []string `json:"input"`
			Model string   `json:"model"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		if body.Model != "voyage-4-lite" || !reflect.DeepEqual(body.Input, []string{"a", "b"}) {
			t.Fatalf("body = %#v", body)
		}
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(`{"data":[{"index":1,"embedding":[0,1]},{"index":0,"embedding":[1,0]}]}`))}, nil
	})}

	got, err := embedder.Embed(t.Context(), []string{"a", "b"})
	if err != nil {
		t.Fatal(err)
	}
	want := [][]float32{{1, 0}, {0, 1}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("vectors = %#v, want %#v", got, want)
	}
	if embedder.MaxBatch() != 1000 {
		t.Fatalf("max batch = %d", embedder.MaxBatch())
	}
}
