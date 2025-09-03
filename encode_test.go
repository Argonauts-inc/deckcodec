// encode_test.go
package deckcodec

import (
	"regexp"
	"slices"
	"sort"
	"testing"
)

// testPack returns a small, ascending card dictionary with the given format ID.
func testPack(formatID uint16) Pack {
	cards := []uint64{
		101, 205, 303, 412,
		501, 602, 703, 804, 905,
		1006, 1107, 1208, 1309, 1410,
		1511, 1612, 1713, 1814, 1915,
		2016, 2117,
		301, 402, 503, 604, 705,
	}
	slices.Sort(cards)
	return Pack{FormatID: formatID, Cards: cards}
}

// equalUint64Slices reports whether two uint64 slices are equal (same length and same elements).
func equalUint64Slices(a, b []uint64) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// equalDeckCounts reports whether two PK->count maps are equal.
func equalDeckCounts(a, b map[uint64]uint8) bool {
	if len(a) != len(b) {
		return false
	}
	for k, va := range a {
		if vb, ok := b[k]; !ok || va != vb {
			return false
		}
	}
	return true
}

// base64URLRe matches Base64URL (raw, no padding): A–Z a–z 0–9 - _
var base64URLRe = regexp.MustCompile(`^[A-Za-z0-9_-]+$`)

// TestEncodeDecode_RoundTrip verifies a normal end-to-end flow:
//  1. Encode a deck into a URL-safe string
//  2. Decode it back
//  3. All sections match (with the expected sorting semantics)
func TestEncodeDecode_RoundTrip(t *testing.T) {
	p := testPack(1)
	in := DeckInput{
		// Intentionally unsorted to assert canonical ordering in the encoded form.
		Leader:  []uint64{412, 205, 101, 303},                     // will be sorted
		Tactics: []uint64{705, 402, 604, 503, 301},                // will be sorted
		Deck:    map[uint64]uint8{804: 1, 703: 2, 602: 3, 501: 4}, // map order irrelevant
	}

	code, err := Encode(p, in)
	if err != nil {
		t.Fatalf("Encode failed: %v", err)
	}
	if !base64URLRe.MatchString(code) {
		t.Fatalf("code contains non-Base64URL characters: %q", code)
	}

	out, err := Decode(p, code)
	if err != nil {
		t.Fatalf("Decode failed: %v", err)
	}
	if out.FormatID != p.FormatID {
		t.Fatalf("FormatID mismatch: got=%d want=%d", out.FormatID, p.FormatID)
	}

	// Canonical ordering: the encoder sorts ordinals. Expect ascending PKs back.
	wantLeader := slices.Clone(in.Leader)
	wantTactics := slices.Clone(in.Tactics)
	slices.Sort(wantLeader)
	slices.Sort(wantTactics)

	if !equalUint64Slices(out.Leader, wantLeader) {
		t.Fatalf("Leader mismatch:\n  got:  %v\n  want: %v", out.Leader, wantLeader)
	}
	if !equalUint64Slices(out.Tactics, wantTactics) {
		t.Fatalf("Tactics mismatch:\n  got:  %v\n  want: %v", out.Tactics, wantTactics)
	}
	if !equalDeckCounts(out.Deck, in.Deck) {
		t.Fatalf("Deck counts mismatch:\n  got:  %#v\n  want: %#v", out.Deck, in.Deck)
	}
}

// TestDeterministicEncoding ensures that logically equivalent inputs (different orderings)
// always yield the exact same encoded string.
func TestDeterministicEncoding(t *testing.T) {
	p := testPack(1)

	inA := DeckInput{
		Leader:  []uint64{101, 205, 303, 412},
		Tactics: []uint64{301, 402, 503, 604, 705},
		Deck:    map[uint64]uint8{501: 4, 602: 3, 703: 2, 804: 1},
	}
	inB := DeckInput{
		Leader:  []uint64{412, 303, 205, 101},                     // reversed
		Tactics: []uint64{705, 604, 503, 402, 301},                // reversed
		Deck:    map[uint64]uint8{804: 1, 703: 2, 602: 3, 501: 4}, // different map iteration
	}

	a, err := Encode(p, inA)
	if err != nil {
		t.Fatalf("Encode(A) failed: %v", err)
	}
	b, err := Encode(p, inB)
	if err != nil {
		t.Fatalf("Encode(B) failed: %v", err)
	}
	if a != b {
		t.Fatalf("determinism violated:\n  A: %q\n  B: %q", a, b)
	}
}

// TestEncode_Errors validates input validation in the encoder:
// - count must be in [1..4]
// - every PK must exist in the pack
func TestEncode_Errors(t *testing.T) {
	p := testPack(1)

	// Count out of range (0)
	_, err := Encode(p, DeckInput{
		Leader:  []uint64{101, 205, 303, 412},
		Tactics: []uint64{301, 402, 503, 604, 705},
		Deck:    map[uint64]uint8{501: 0},
	})
	if err == nil {
		t.Fatalf("expected error for count=0, got nil")
	}

	// Count out of range (5)
	_, err = Encode(p, DeckInput{
		Leader:  []uint64{101, 205, 303, 412},
		Tactics: []uint64{301, 402, 503, 604, 705},
		Deck:    map[uint64]uint8{501: 5},
	})
	if err == nil {
		t.Fatalf("expected error for count=5, got nil")
	}

	// Unknown PK
	_, err = Encode(p, DeckInput{
		Leader:  []uint64{101, 205, 303, 412},
		Tactics: []uint64{301, 402, 503, 604, 705},
		Deck:    map[uint64]uint8{999999: 1},
	})
	if err == nil {
		t.Fatalf("expected error for unknown PK, got nil")
	}
}

// TestDecode_FormatIDMismatch checks that decoding with a different pack
// (same cards but different format_id) fails cleanly.
func TestDecode_FormatIDMismatch(t *testing.T) {
	p1 := testPack(1)
	p2 := testPack(2) // same cards, different format_id

	in := DeckInput{
		Leader:  []uint64{101, 205, 303, 412},
		Tactics: []uint64{301, 402, 503, 604, 705},
		Deck:    map[uint64]uint8{501: 4, 602: 3},
	}

	code, err := Encode(p1, in)
	if err != nil {
		t.Fatalf("Encode failed: %v", err)
	}
	if _, err := Decode(p2, code); err == nil {
		t.Fatalf("expected format_id mismatch error, got nil")
	}
}

// TestEmptySections verifies that empty leader/tactics are supported by the codec,
// even if real game rules might fix those counts. This documents the serialization behavior.
func TestEmptySections(t *testing.T) {
	p := testPack(1)
	in := DeckInput{
		Leader:  nil,                      // 0
		Tactics: []uint64{},               // 0
		Deck:    map[uint64]uint8{501: 1}, // minimal deck content
	}

	code, err := Encode(p, in)
	if err != nil {
		t.Fatalf("Encode failed: %v", err)
	}
	out, err := Decode(p, code)
	if err != nil {
		t.Fatalf("Decode failed: %v", err)
	}
	if len(out.Leader) != 0 || len(out.Tactics) != 0 {
		t.Fatalf("expected empty sections, got L=%d T=%d", len(out.Leader), len(out.Tactics))
	}
	if !equalDeckCounts(out.Deck, in.Deck) {
		t.Fatalf("deck mismatch: got=%#v want=%#v", out.Deck, in.Deck)
	}
}

// TestBase64URLSafety asserts that produced codes never include URL-problematic characters
// like '/', '+', '=', '#', or '?' (we use Base64URL without padding).
func TestBase64URLSafety(t *testing.T) {
	p := testPack(1)
	in := DeckInput{
		Leader:  []uint64{101, 205, 303, 412},
		Tactics: []uint64{301, 402, 503, 604, 705},
		Deck:    map[uint64]uint8{501: 4, 602: 3, 703: 2, 804: 1},
	}
	code, err := Encode(p, in)
	if err != nil {
		t.Fatalf("Encode failed: %v", err)
	}
	if !base64URLRe.MatchString(code) {
		t.Fatalf("code is not Base64URL-safe: %q", code)
	}
}

// (Optional) TestLengthMonotonicity documents that larger dictionaries (M ↑ -> id_bits ↑)
// tend to make the encoded string longer, all else being equal. We don't assert an exact
// length (depends on byte packing boundaries), but we expect non-decreasing behavior.
func TestLengthMonotonicity(t *testing.T) {
	// Start with a small pack (M=32) and extend it.
	makePackWithM := func(fid uint16, m int) Pack {
		cards := make([]uint64, m)
		for i := 0; i < m; i++ {
			cards[i] = uint64(1000 + i*3) // arbitrary ascending PKs
		}
		return Pack{FormatID: fid, Cards: cards}
	}

	in := DeckInput{
		Leader:  []uint64{1000, 1003, 1006, 1009},
		Tactics: []uint64{1012, 1015, 1018, 1021, 1024},
		Deck:    map[uint64]uint8{1027: 1, 1030: 2, 1033: 3, 1036: 4},
	}

	lengths := make([]int, 0, 4)
	for _, m := range []int{32, 64, 128, 256} {
		p := makePackWithM(10+uint16(m), m)
		code, err := Encode(p, in)
		if err != nil {
			t.Fatalf("Encode failed for M=%d: %v", m, err)
		}
		lengths = append(lengths, len(code))
	}

	// Non-decreasing
	for i := 1; i < len(lengths); i++ {
		if lengths[i] < lengths[i-1] {
			t.Fatalf("code length decreased: %v", lengths)
		}
	}
	_ = sort.Ints // keep import if needed later
}
