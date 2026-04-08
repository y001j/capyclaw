package ripple

import (
	"math"
	"time"
)

// Reranker applies MMR diversity and temporal decay to search results.
type Reranker struct {
	temporalHalfLife time.Duration
}

// NewReranker creates a new reranker with temporal decay.
func NewReranker(halfLife time.Duration) *Reranker {
	return &Reranker{temporalHalfLife: halfLife}
}

// ApplyTemporalDecay adjusts scores based on how old the memory is.
func (r *Reranker) ApplyTemporalDecay(score float64, createdAt time.Time) float64 {
	age := time.Since(createdAt)
	decayFactor := math.Pow(0.5, float64(age)/float64(r.temporalHalfLife))
	return score * decayFactor
}

// MMRRerank applies Maximal Marginal Relevance for diversity.
func (r *Reranker) MMRRerank(results []SearchResult, lambda float64) []SearchResult {
	if len(results) <= 1 {
		return results
	}

	// TODO: implement MMR algorithm
	// 1. Select highest scored result first
	// 2. For each remaining result, compute:
	//    MMR = λ * relevance - (1-λ) * max_similarity_to_selected
	// 3. Select the result with highest MMR score
	// 4. Repeat until all results are ranked

	return results
}
