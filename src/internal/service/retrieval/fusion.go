// Package retrieval implements the hybrid retrieval pipeline.
package retrieval

import (
	"sort"

	"github.com/google/uuid"

	"memory-service/internal/adapters/store"
)

const rrfK = 60

type RRFInput struct {
	Memories []store.ScoredMemory
}

// FuseRRF combines multiple ranked lists using Reciprocal Rank Fusion.
//
// Formula per memory m:
//
//	RRF_score(m) = Σ_channels  1 / (rrfK + rank_in_channel(m))
//
// Memories not present in a channel are simply not added.
// Returns a deduplicated list sorted by RRF score descending.
// Limit caps the returned slice.
func FuseRRF(inputs []RRFInput, limit int) []store.ScoredMemory {
	scores := make(map[uuid.UUID]float32)
	byID := make(map[uuid.UUID]store.ScoredMemory)

	for _, input := range inputs {
		for rank, m := range input.Memories {
			rrfScore := float32(1.0) / float32(rrfK+rank+1)
			scores[m.ID] += rrfScore
			byID[m.ID] = m
		}
	}

	fused := make([]store.ScoredMemory, 0, len(scores))
	for id, score := range scores {
		m := byID[id]
		m.Score = score
		fused = append(fused, m)
	}

	sort.Slice(fused, func(i, j int) bool {
		return fused[i].Score > fused[j].Score
	})

	if limit > 0 && len(fused) > limit {
		return fused[:limit]
	}
	return fused
}
