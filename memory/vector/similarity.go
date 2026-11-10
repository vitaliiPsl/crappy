package vector

import "math"

// Similarity scores finite, nonzero vectors of equal length. Higher scores rank first.
// Calls may run concurrently; input slices must remain unchanged.
type Similarity func(query, document []float32) float64

func Cosine(query, document []float32) float64 {
	var dot, queryNorm, documentNorm float64
	for position, value := range query {
		q, d := float64(value), float64(document[position])
		dot += q * d
		queryNorm += q * q
		documentNorm += d * d
	}

	return max(-1, min(1, dot/(math.Sqrt(queryNorm)*math.Sqrt(documentNorm))))
}
