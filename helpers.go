package deckcodec

import "slices"

// UniqSortedPKsFromDeck returns a sorted unique list of PKs
// collected from (Leader, Tactics, Deck keys).
// The result is safe to reuse as a set-like input for Bloom prefiltering.
func UniqSortedPKsFromDeck(in DeckInput) []uint64 {
	// Pre-size: leaders + tactics + unique deck keys
	out := make([]uint64, 0, len(in.Leader)+len(in.Tactics)+len(in.Deck))
	out = append(out, in.Leader...)
	out = append(out, in.Tactics...)
	for pk := range in.Deck {
		out = append(out, pk)
	}
	if len(out) == 0 {
		return out
	}
	slices.Sort(out)

	// In-place dedup
	w := 1
	for i := 1; i < len(out); i++ {
		if out[i] != out[w-1] {
			out[w] = out[i]
			w++
		}
	}
	return out[:w]
}

// MayContainAll returns true if Bloom "may" contain all PKs (Bloom semantics).
// If b is nil or empty, it returns true (do-not-filter behavior).
func MayContainAll(b *BloomMeta, pks []uint64) bool {
	for _, pk := range pks {
		if b != nil && !b.MayContain(pk) {
			return false
		}
	}
	return true
}
