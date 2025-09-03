package deckcodec

import (
	"encoding/base64"
	"slices"
	"testing"
)

// TestUniqSortedPKsFromDeck verifies collection, sorting, and in-place dedup.
func TestUniqSortedPKsFromDeck(t *testing.T) {
	in := DeckInput{
		Leader:  []uint64{10, 1, 10}, // duplicates + unsorted
		Tactics: []uint64{3, 2, 3},
		Deck:    map[uint64]uint8{5: 1, 4: 2}, // keys only matter
	}
	got := UniqSortedPKsFromDeck(in)
	want := []uint64{1, 2, 3, 4, 5, 10}
	if !slices.Equal(got, want) {
		t.Fatalf("uniq-sorted mismatch:\n  got:  %v\n  want: %v", got, want)
	}

	// Empty input → empty output
	empty := UniqSortedPKsFromDeck(DeckInput{})
	if len(empty) != 0 {
		t.Fatalf("expected empty result, got %v", empty)
	}
}

// TestMayContainAll_NilOrEmptyBloom ensures do-not-filter behavior.
func TestMayContainAll_NilOrEmptyBloom(t *testing.T) {
	pks := []uint64{1, 2, 3}

	// nil bloom → always true
	var b *BloomMeta
	if !MayContainAll(b, pks) {
		t.Fatalf("nil bloom should return true")
	}

	// empty bloom (MBits=0, K=0) → always true
	b = &BloomMeta{}
	if !MayContainAll(b, pks) {
		t.Fatalf("empty bloom should return true")
	}
}

// TestMayContainAll_TrueForMembers uses a real Bloom built from cards
// and verifies members are reported as "may contain".
func TestMayContainAll_TrueForMembers(t *testing.T) {
	cards := []uint64{100, 101, 102, 103, 104}
	slices.Sort(cards)

	bl, err := buildBloomForCards(cards, 0.01) // ~1% FPR
	if err != nil {
		t.Fatalf("buildBloomForCards error: %v", err)
	}
	if !MayContainAll(bl, cards) {
		t.Fatalf("members must all pass MayContainAll")
	}

	// A subset should also pass.
	sub := []uint64{100, 104}
	if !MayContainAll(bl, sub) {
		t.Fatalf("subset members must pass MayContainAll")
	}
}

// TestMayContainAll_FalseWhenDefinitelyAbsent crafts a Bloom with all bits zero,
// guaranteeing any non-empty PK list will fail. This tests the false path
// deterministically without relying on probability.
func TestMayContainAll_FalseWhenDefinitelyAbsent(t *testing.T) {
	// A BloomMeta with non-zero MBits/K but zeroed bit array.
	// Any pk will hash into at least one zero bit → MayContain=false.
	bits := make([]byte, 8) // 64 bits, all zero
	bl := &BloomMeta{
		MBits:   64,
		K:       2,
		Salt1:   0x9e3779b97f4a7c15,
		Salt2:   0xbf58476d1ce4e5b9,
		BitsB64: base64.RawURLEncoding.EncodeToString(bits),
	}
	if MayContainAll(bl, []uint64{42}) {
		t.Fatalf("expected MayContainAll=false for zeroed bloom")
	}
}
