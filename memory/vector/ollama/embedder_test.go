package ollama_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/vitaliipsl/crappy/memory/vector/ollama"
)

func TestEmbedderFormatsQueriesAndDocuments(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		name           string
		queryPrefix    string
		documentPrefix string
	}{
		{name: "plain"},
		{name: "Qwen", queryPrefix: "Instruct: Retrieve relevant memories.\nQuery: "},
		{name: "Nomic", queryPrefix: "search_query: ", documentPrefix: "search_document: "},
	} {
		t.Run(test.name, func(t *testing.T) {
			inputs := make(chan []string, 2)
			server := newServer(t, func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodPost || r.Header.Get("Content-Type") != "application/json" {
					t.Errorf("embedding request = %s, %v", r.Method, r.Header)
				}

				var request struct {
					Model    string   `json:"model"`
					Input    []string `json:"input"`
					Truncate *bool    `json:"truncate"`
				}
				if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
					t.Error(err)

					return
				}

				if request.Model != "local" || request.Truncate == nil || *request.Truncate {
					t.Errorf("embedding request = %+v", request)
				}

				inputs <- request.Input

				values := make([][]float32, len(request.Input))
				for position := range values {
					values[position] = []float32{float32(position + 1), 1}
				}

				if err := json.NewEncoder(w).Encode(map[string]any{"model": "local:latest", "embeddings": values}); err != nil {
					t.Error(err)
				}
			})

			embedder := newEmbedder(t, server, ollama.WithQueryPrefix(test.queryPrefix), ollama.WithDocumentPrefix(test.documentPrefix))
			texts := []string{"saved fact", "another fact"}

			documents, err := embedder.EmbedDocuments(t.Context(), texts)
			if err != nil || len(documents) != 2 || !slices.Equal(documents[0].Values, []float32{1, 1}) || !slices.Equal(documents[1].Values, []float32{2, 1}) {
				t.Fatalf("document embeddings = %+v, %v", documents, err)
			}

			if got := <-inputs; !slices.Equal(got, []string{test.documentPrefix + texts[0], test.documentPrefix + texts[1]}) {
				t.Fatalf("document inputs = %v", got)
			}

			if !slices.Equal(texts, []string{"saved fact", "another fact"}) {
				t.Fatalf("caller inputs were changed: %v", texts)
			}

			query, err := embedder.EmbedQuery(t.Context(), "a question")
			if err != nil || !slices.Equal(query.Values, []float32{1, 1}) {
				t.Fatalf("query embedding = %+v, %v", query, err)
			}

			if got := <-inputs; !slices.Equal(got, []string{test.queryPrefix + "a question"}) {
				t.Fatalf("query input = %v", got)
			}

			for _, metadata := range []map[string]json.RawMessage{documents[0].Metadata, documents[1].Metadata, query.Metadata} {
				if string(metadata["provider"]) != `"ollama"` || string(metadata["model"]) != `"local:latest"` || len(metadata) != 2 {
					t.Fatalf("embedding provenance = %v", metadata)
				}
			}

			documents[0].Metadata["model"][1] = 'x'
			if string(documents[1].Metadata["model"]) != `"local:latest"` {
				t.Fatal("embeddings share mutable metadata")
			}

			empty, err := embedder.EmbedDocuments(t.Context(), nil)
			if err != nil || len(empty) != 0 {
				t.Fatalf("empty batch = %+v, %v", empty, err)
			}

			select {
			case got := <-inputs:
				t.Fatalf("empty batch requested embeddings for %v", got)
			default:
			}
		})
	}
}

func TestEmbedderSpaceTracksModelAndFormatting(t *testing.T) {
	t.Parallel()

	unexpectedRequest := func(w http.ResponseWriter, _ *http.Request) {
		t.Error("construction contacted Ollama")
		w.WriteHeader(http.StatusInternalServerError)
	}
	server := newServer(t, unexpectedRequest)
	first := newEmbedder(t, server)

	second := newEmbedder(t, newServer(t, unexpectedRequest))
	if first.Space() == "" || first.Space() != second.Space() {
		t.Fatalf("identical configuration has different spaces: %q, %q", first.Space(), second.Space())
	}

	other, err := ollama.New("other", ollama.WithBaseURL(server.URL))
	if err != nil {
		t.Fatal(err)
	}

	for _, embedder := range []*ollama.Embedder{
		newEmbedder(t, server, ollama.WithQueryPrefix("query: ")),
		newEmbedder(t, server, ollama.WithDocumentPrefix("document: ")),
		other,
	} {
		if embedder.Space() == first.Space() {
			t.Fatal("different models or formatting reused the same space")
		}
	}
}

func TestNewRejectsInvalidConfiguration(t *testing.T) {
	t.Parallel()

	server := newServer(t, func(w http.ResponseWriter, _ *http.Request) {
		t.Error("invalid constructor requested embeddings")
		w.WriteHeader(http.StatusInternalServerError)
	})
	for _, test := range []struct {
		model   string
		baseURL string
	}{
		{model: "", baseURL: server.URL},
		{model: " \t", baseURL: server.URL},
		{model: "local", baseURL: "not-a-url"},
		{model: "local", baseURL: "file:///tmp/ollama"},
	} {
		if embedder, err := ollama.New(test.model, ollama.WithBaseURL(test.baseURL)); err == nil || embedder != nil {
			t.Fatalf("invalid model=%q, URL=%q accepted: %v, %v", test.model, test.baseURL, embedder, err)
		}
	}
}

func TestEmbedderPropagatesInvalidResponses(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		name      string
		status    int
		body      string
		wantError string
	}{
		{name: "provider error", status: http.StatusBadRequest, body: `{"error":"input exceeds context window"}`, wantError: "input exceeds context window"},
		{name: "missing installed model", status: http.StatusNotFound, body: `{"error":"model is not installed"}`, wantError: "model is not installed"},
		{name: "non-JSON error", status: http.StatusBadGateway, body: "bad gateway", wantError: "502"},
		{name: "invalid JSON", status: http.StatusOK, body: `{`, wantError: "decode"},
		{name: "missing embedding", status: http.StatusOK, body: `{"model":"local:latest","embeddings":[]}`, wantError: "embeddings"},
		{name: "missing model", status: http.StatusOK, body: `{"embeddings":[[1,0]]}`, wantError: "model"},
	} {
		t.Run(test.name, func(t *testing.T) {
			server := newServer(t, func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(test.status)

				if _, err := fmt.Fprint(w, test.body); err != nil {
					t.Error(err)
				}
			})
			embedder := newEmbedder(t, server)

			query, err := embedder.EmbedQuery(t.Context(), "question")
			if err == nil || !strings.Contains(err.Error(), test.wantError) || query.Values != nil {
				t.Fatalf("invalid query response = %+v, %v", query, err)
			}

			documents, err := embedder.EmbedDocuments(t.Context(), []string{"document"})
			if err == nil || !strings.Contains(err.Error(), test.wantError) || documents != nil {
				t.Fatalf("invalid document response = %+v, %v", documents, err)
			}
		})
	}
}

func TestEmbedderHonorsCancellation(t *testing.T) {
	t.Parallel()

	started := make(chan struct{})
	server := newServer(t, func(_ http.ResponseWriter, r *http.Request) {
		if _, err := io.Copy(io.Discard, r.Body); err != nil {
			t.Error(err)

			return
		}

		close(started)
		<-r.Context().Done()
	})
	embedder := newEmbedder(t, server)

	timeout, stop := context.WithTimeout(t.Context(), 5*time.Second)
	defer stop()

	ctx, cancel := context.WithCancel(timeout)
	defer cancel()

	finished := make(chan error, 1)
	go func() {
		_, err := embedder.EmbedQuery(ctx, "question")
		finished <- err
	}()

	select {
	case <-started:
	case <-timeout.Done():
		t.Fatal(timeout.Err())
	}

	cancel()

	select {
	case err := <-finished:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("canceled query = %v", err)
		}
	case <-timeout.Done():
		t.Fatal(timeout.Err())
	}

	if _, err := embedder.EmbedDocuments(ctx, nil); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled batch = %v", err)
	}
}

func newServer(t *testing.T, embed http.HandlerFunc) *httptest.Server {
	t.Helper()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		if r.URL.Path != "/api/embed" {
			t.Errorf("unexpected path: %s", r.URL.Path)
			http.NotFound(w, r)

			return
		}

		embed(w, r)
	}))
	t.Cleanup(server.Close)

	return server
}

func newEmbedder(t *testing.T, server *httptest.Server, options ...ollama.Option) *ollama.Embedder {
	t.Helper()

	options = append([]ollama.Option{ollama.WithBaseURL(server.URL), ollama.WithHTTPClient(server.Client())}, options...)

	embedder, err := ollama.New("local", options...)
	if err != nil {
		t.Fatal(err)
	}

	return embedder
}
