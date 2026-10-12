package concurrent

import (
	"math"
	"regexp"
	"slices"
)

// EmbedTolerance is how far below 1 a batched vector's cosine to its input's lone embedding may
// fall: batching moved EmbeddingGemma 2's cosine by at most 1.5e-4 on b11572, while two distinct
// inputs of the run's sat no closer than 0.67, so 1e-3 passes batching noise and fails a swap.
const EmbedTolerance = 1e-3

// noncePrefix starts every nonce; nonceRE matches any nonce, so an answer is scanned for every
// nonce it carries, the run's or not.
const noncePrefix = "nonce-"

var nonceRE = regexp.MustCompile(noncePrefix + `[0-9a-f]{16}`)

// containsNonce reports whether answer carries nonce as a whole token.
func containsNonce(answer, nonce string) bool {
	return slices.Contains(nonceRE.FindAllString(answer, -1), nonce)
}

// checkNonces records, for each chat, the other requests whose nonces its answer carries, and
// the nonces it carries that no request in the run sent.
func checkNonces(chats []ChatResult) {
	owner := make(map[string]string, len(chats))
	for _, c := range chats {
		owner[c.Nonce] = c.ID
	}
	for i := range chats {
		c := &chats[i]
		c.Foreign, c.Unknown = nil, nil
		for _, n := range nonceRE.FindAllString(c.Answer, -1) {
			id, ok := owner[n]
			switch {
			case !ok:
				if !slices.Contains(c.Unknown, n) {
					c.Unknown = append(c.Unknown, n)
				}
			case id != c.ID && !slices.Contains(c.Foreign, id):
				c.Foreign = append(c.Foreign, id)
			}
		}
	}
}

// Cosine returns the cosine similarity of a and b, and 0 when they differ in length or either
// is zero.
func Cosine(a, b []float32) float64 {
	if len(a) != len(b) {
		return 0
	}
	var dot, na, nb float64
	for i := range a {
		x, y := float64(a[i]), float64(b[i])
		dot += x * y
		na += x * x
		nb += y * y
	}
	if na == 0 || nb == 0 {
		return 0
	}
	return dot / math.Sqrt(na*nb)
}
