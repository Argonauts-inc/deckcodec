package deckcodec

import (
	"encoding/base64"
	"errors"
	"slices"
	"sort"

	bitio "github.com/Argonauts-inc/deckcodec/internal"
)

type DeckInput struct {
	Leader  []uint64
	Tactics []uint64
	Deck    map[uint64]uint8
}

type DeckOutput struct {
	FormatID uint16
	Leader   []uint64
	Tactics  []uint64
	Deck     map[uint64]uint8
}

// idBits returns the minimum number of bits required to represent m distinct values.
// For example, if m=5, it returns 3 because 3 bits can represent up to 8 values.
func idBits(m int) int {
	if m <= 1 {
		return 1
	}
	b := 0
	for (1 << b) < m {
		b++
	}
	return b
}

// ordinalOf returns the index (ordinal) of pk in the sorted cards slice, and true if found.
// If pk is not found, returns the index where it would be inserted and false.
func ordinalOf(cards []uint64, pk uint64) (uint32, bool) {
	i := sort.Search(len(cards), func(i int) bool { return cards[i] >= pk })
	return uint32(i), i < len(cards) && cards[i] == pk
}

// Encode encodes a deck (DeckInput) into a compact base64 string using the provided Pack definition.
// The encoding includes the format ID, leader cards, tactics cards, and the main deck with counts.
// Returns the encoded string or an error if the input is invalid.
func Encode(p Pack, in DeckInput) (string, error) {
	// Check for valid pack format and card list
	if p.FormatID == 0 {
		return "", errors.New("deckcodec: pack.FormatID must be non-zero")
	}
	if len(p.Cards) == 0 {
		return "", errors.New("deckcodec: empty pack")
	}
	// Calculate the number of bits needed to represent a card ordinal
	ib := idBits(len(p.Cards))

	// Helper function to convert a slice of card PKs to their ordinals in the pack
	toOrd := func(pks []uint64) ([]uint32, error) {
		out := make([]uint32, 0, len(pks))
		for _, pk := range pks {
			o, ok := ordinalOf(p.Cards, pk)
			if !ok {
				return nil, errors.New("deckcodec: pk not in pack")
			}
			out = append(out, o)
		}
		// Sort ordinals to ensure deterministic encoding
		slices.Sort(out)
		return out, nil
	}
	// Convert leader and tactics PKs to ordinals
	L, err := toOrd(in.Leader)
	if err != nil {
		return "", err
	}
	T, err := toOrd(in.Tactics)
	if err != nil {
		return "", err
	}

	// Prepare the main deck as a slice of (ordinal, count) pairs
	type pair struct {
		o uint32
		c uint8
	}
	P := make([]pair, 0, len(in.Deck))
	for pk, c := range in.Deck {
		// Only allow card counts between 1 and 4
		if c < 1 || c > 4 {
			return "", errors.New("deckcodec: count out of range (1..4)")
		}
		o, ok := ordinalOf(p.Cards, pk)
		if !ok {
			return "", errors.New("deckcodec: pk not in pack")
		}
		P = append(P, pair{o: o, c: c})
	}
	// Sort deck pairs by ordinal for deterministic encoding
	sort.Slice(P, func(i, j int) bool { return P[i].o < P[j].o })

	var bw bitio.Writer
	// Write header: 16 bits for format ID
	bw.WriteBits(uint32(p.FormatID), 16)

	// Write leader section: 8 bits for count, then each ordinal
	if len(L) > 255 {
		return "", errors.New("deckcodec: leader too long")
	}
	bw.WriteBits(uint32(len(L)), 8)
	for _, o := range L {
		bw.WriteBits(o, ib)
	}

	// Write tactics section: 8 bits for count, then each ordinal
	if len(T) > 255 {
		return "", errors.New("deckcodec: tactics too long")
	}
	bw.WriteBits(uint32(len(T)), 8)
	for _, o := range T {
		bw.WriteBits(o, ib)
	}

	// Write deck section: 8 bits for unique card count, then each (ordinal, count-1) pair
	if len(P) > 255 {
		return "", errors.New("deckcodec: deck unique too long")
	}
	bw.WriteBits(uint32(len(P)), 8)
	for _, pr := range P {
		bw.WriteBits(pr.o, ib)          // Write card ordinal
		bw.WriteBits(uint32(pr.c-1), 2) // Write count minus 1 (so 1..4 becomes 0..3)
	}

	// Finalize bit stream and encode as base64 (URL-safe, no padding)
	raw := bw.Finish()
	return base64.RawURLEncoding.EncodeToString(raw), nil
}

// Decode decodes a base64-encoded deck string into a DeckOutput using the provided Pack definition.
// Returns the decoded deck or an error if the code is invalid or does not match the pack.
func Decode(p Pack, code string) (DeckOutput, error) {
	// Decode base64 string to raw bytes
	raw, err := base64.RawURLEncoding.DecodeString(code)
	if err != nil {
		return DeckOutput{}, err
	}
	// Calculate the number of bits needed to represent a card ordinal
	ib := idBits(len(p.Cards))

	// Initialize bit reader
	br := bitio.NewReader(raw, len(raw)*8)
	// Read and check format ID (16 bits)
	fid, err := br.ReadBits(16)
	if err != nil {
		return DeckOutput{}, err
	}
	if uint16(fid) != p.FormatID {
		return DeckOutput{}, errors.New("deckcodec: format_id mismatch")
	}

	// Helper function to convert an ordinal to a PK (card ID)
	toPK := func(o uint32) (uint64, error) {
		if int(o) >= len(p.Cards) {
			return 0, errors.New("deckcodec: ordinal OOB")
		}
		return p.Cards[o], nil
	}

	// Read leader section: 8 bits for count, then each ordinal
	nL, err := br.ReadBits(8)
	if err != nil {
		return DeckOutput{}, err
	}
	L := make([]uint64, nL)
	for i := 0; i < int(nL); i++ {
		o, err := br.ReadBits(ib)
		if err != nil {
			return DeckOutput{}, err
		}
		pk, err := toPK(o)
		if err != nil {
			return DeckOutput{}, err
		}
		L[i] = pk
	}

	// Read tactics section: 8 bits for count, then each ordinal
	nT, err := br.ReadBits(8)
	if err != nil {
		return DeckOutput{}, err
	}
	T := make([]uint64, nT)
	for i := 0; i < int(nT); i++ {
		o, err := br.ReadBits(ib)
		if err != nil {
			return DeckOutput{}, err
		}
		pk, err := toPK(o)
		if err != nil {
			return DeckOutput{}, err
		}
		T[i] = pk
	}

	// Read deck section: 8 bits for unique card count, then each (ordinal, count-1) pair
	nD, err := br.ReadBits(8)
	if err != nil {
		return DeckOutput{}, err
	}
	D := make(map[uint64]uint8, nD)
	for i := 0; i < int(nD); i++ {
		o, err := br.ReadBits(ib)
		if err != nil {
			return DeckOutput{}, err
		}
		cm1, err := br.ReadBits(2)
		if err != nil {
			return DeckOutput{}, err
		}
		pk, err := toPK(o)
		if err != nil {
			return DeckOutput{}, err
		}
		D[pk] = uint8(cm1) + 1 // Convert stored count-1 back to count (1..4)
	}

	// Return the decoded deck structure
	return DeckOutput{FormatID: p.FormatID, Leader: L, Tactics: T, Deck: D}, nil
}
