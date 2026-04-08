package ripple

import (
	"context"
)

// EmbeddingGenerator generates vector embeddings for text.
type EmbeddingGenerator interface {
	Generate(ctx context.Context, text string) ([]float32, error)
	Dimensions() int
}

// CachedEmbedder wraps an embedding generator with caching.
type CachedEmbedder struct {
	inner EmbeddingGenerator
	// TODO: add LRU cache for frequently used embeddings
}

// NewCachedEmbedder creates a cached embedding generator.
func NewCachedEmbedder(inner EmbeddingGenerator) *CachedEmbedder {
	return &CachedEmbedder{inner: inner}
}

func (c *CachedEmbedder) Generate(ctx context.Context, text string) ([]float32, error) {
	// TODO: check cache first, then generate and cache
	return c.inner.Generate(ctx, text)
}

func (c *CachedEmbedder) Dimensions() int {
	return c.inner.Dimensions()
}
