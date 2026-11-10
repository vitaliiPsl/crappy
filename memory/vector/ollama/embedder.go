package ollama

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/vitaliipsl/crappy/memory/vector"
)

var _ vector.Embedder = (*Embedder)(nil)

type Option func(*Embedder)

func WithBaseURL(baseURL string) Option {
	return func(embedder *Embedder) {
		embedder.baseURL = baseURL
	}
}

func WithHTTPClient(client *http.Client) Option {
	return func(embedder *Embedder) {
		embedder.client = client
	}
}

func WithQueryPrefix(prefix string) Option {
	return func(embedder *Embedder) {
		embedder.queryPrefix = prefix
	}
}

func WithDocumentPrefix(prefix string) Option {
	return func(embedder *Embedder) {
		embedder.documentPrefix = prefix
	}
}

type Embedder struct {
	model          string
	space          string
	queryPrefix    string
	documentPrefix string

	baseURL string
	client  *http.Client
}

func New(model string, options ...Option) (*Embedder, error) {
	if strings.TrimSpace(model) == "" {
		return nil, errors.New("embedding model is required")
	}

	embedder := &Embedder{model: model, baseURL: "http://localhost:11434", client: http.DefaultClient}
	for _, option := range options {
		option(embedder)
	}

	baseURL, err := url.Parse(embedder.baseURL)
	if err != nil {
		return nil, fmt.Errorf("parse Ollama URL: %w", err)
	}

	if (baseURL.Scheme != "http" && baseURL.Scheme != "https") || baseURL.Host == "" || baseURL.RawQuery != "" || baseURL.Fragment != "" {
		return nil, errors.New("ollama URL must be an HTTP or HTTPS base URL without a query or fragment")
	}

	embedder.baseURL = strings.TrimRight(baseURL.String(), "/")
	identity, _ := json.Marshal([]string{embedder.model, embedder.queryPrefix, embedder.documentPrefix})
	embedder.space = fmt.Sprintf("ollama:%x", sha256.Sum256(identity))

	return embedder, nil
}

func (e *Embedder) Space() string {
	return e.space
}

func (e *Embedder) EmbedQuery(ctx context.Context, text string) (vector.Embedding, error) {
	embeddings, err := e.embed(ctx, []string{e.queryPrefix + text})
	if err != nil {
		return vector.Embedding{}, err
	}

	return embeddings[0], nil
}

func (e *Embedder) EmbedDocuments(ctx context.Context, texts []string) ([]vector.Embedding, error) {
	inputs := make([]string, len(texts))
	for position, text := range texts {
		inputs[position] = e.documentPrefix + text
	}

	return e.embed(ctx, inputs)
}

func (e *Embedder) embed(ctx context.Context, texts []string) ([]vector.Embedding, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	if len(texts) == 0 {
		return []vector.Embedding{}, nil
	}

	payload := struct {
		Model    string   `json:"model"`
		Input    []string `json:"input"`
		Truncate bool     `json:"truncate"`
	}{Model: e.model, Input: texts, Truncate: false}

	data, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("encode ollama request: %w", err)
	}

	request, err := http.NewRequestWithContext(ctx, http.MethodPost, e.baseURL+"/api/embed", bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("create ollama request: %w", err)
	}

	request.Header.Set("Content-Type", "application/json")

	response, err := e.client.Do(request)
	if err != nil {
		return nil, fmt.Errorf("ollama embed: %w", err)
	}

	defer func() { _ = response.Body.Close() }()

	if response.StatusCode < 200 || response.StatusCode >= 300 {
		var failure struct {
			Error string `json:"error"`
		}
		if err := json.NewDecoder(response.Body).Decode(&failure); err == nil && failure.Error != "" {
			return nil, fmt.Errorf("ollama embed: %s: %s", response.Status, failure.Error)
		}

		return nil, fmt.Errorf("ollama embed: %s", response.Status)
	}

	var result struct {
		Model      string      `json:"model"`
		Embeddings [][]float32 `json:"embeddings"`
	}
	if err := json.NewDecoder(response.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decode ollama embedding response: %w", err)
	}

	if len(result.Embeddings) != len(texts) {
		return nil, fmt.Errorf("ollama returned %d embeddings for %d inputs", len(result.Embeddings), len(texts))
	}

	if result.Model == "" {
		return nil, errors.New("ollama embedding response has no model")
	}

	embeddings := make([]vector.Embedding, len(texts))
	for position, values := range result.Embeddings {
		model, _ := json.Marshal(result.Model)
		embeddings[position] = vector.Embedding{
			Values: values,
			Metadata: map[string]json.RawMessage{
				"provider": json.RawMessage(`"ollama"`),
				"model":    model,
			},
		}
	}

	return embeddings, nil
}
