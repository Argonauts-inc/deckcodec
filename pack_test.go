package deckcodec

import (
	"bytes"
	"encoding/json"
	"slices"
	"testing"
	"time"
)

//
// ------------------------- ParsePack tests -------------------------
//

// TestParsePack_SortsAndValid verifies that ParsePack:
// - decodes JSON from any io.Reader
// - enforces non-zero FormatID
// - sorts Cards ascending (duplicates preserved)
func TestParsePack_SortsAndValid(t *testing.T) {
	src := `{
	  "format_id": 7,
	  "name": "Std 2025-09",
	  "cards": [5,3,7,3,2,9]
	}`
	p, err := ParsePack(bytes.NewBufferString(src))
	if err != nil {
		t.Fatalf("ParsePack error: %v", err)
	}
	if p.FormatID != 7 || p.Name != "Std 2025-09" {
		t.Fatalf("meta mismatch: %+v", p)
	}
	// Duplicates must be preserved; order must be ascending
	want := []uint64{2, 3, 3, 5, 7, 9}
	if !slices.Equal(p.Cards, want) {
		t.Fatalf("cards mismatch: got=%v want=%v", p.Cards, want)
	}
}

// TestParsePack_Errors checks typical error cases:
// - invalid JSON
// - unknown field (DisallowUnknownFields)
// - zero FormatID
func TestParsePack_Errors(t *testing.T) {
	// Invalid JSON (trailing comma)
	if _, err := ParsePack(bytes.NewBufferString(`{"format_id":1,"cards":[1,2,]}`)); err == nil {
		t.Fatalf("expected error for invalid JSON, got nil")
	}
	// Unknown field
	if _, err := ParsePack(bytes.NewBufferString(`{"format_id":1,"cards":[1],"oops":true}`)); err == nil {
		t.Fatalf("expected error for unknown field, got nil")
	}
	// Zero format_id
	if _, err := ParsePack(bytes.NewBufferString(`{"format_id":0,"cards":[1,2,3]}`)); err == nil {
		t.Fatalf("expected error for zero format_id, got nil")
	}
}

//
// ----------------------- BuildManifest tests ----------------------
//

// mkPack is a small helper to construct a Pack value directly (unsorted on purpose).
func mkPack(fid uint16, name string, cards []uint64) Pack {
	return Pack{
		FormatID: fid,
		Name:     name,
		Cards:    slices.Clone(cards), // BuildManifest will sort internally
	}
}

// itoa is a tiny helper for stable URLs in tests (avoid strconv import).
func itoa(fid uint16) string {
	d := int(fid)
	if d == 0 {
		return "0"
	}
	var buf [6]byte
	i := len(buf)
	for d > 0 {
		i--
		buf[i] = byte('0' + d%10)
		d /= 10
	}
	return string(buf[i:])
}

// TestBuildManifest_HappyPath verifies:
// - packs sorted by M asc (tie-break by FormatID)
// - urlFor callback is applied
// - Bloom attached when targetFP > 0 with no false negatives
// - JSON marshal works
func TestBuildManifest_HappyPath(t *testing.T) {
	// Three packs with sizes: 2, 3, 5 (cards intentionally unsorted)
	p3 := mkPack(3, "Micro", []uint64{9, 8})                     // M=2
	p1 := mkPack(1, "Std 2025-09", []uint64{303, 101, 205})      // M=3
	p2 := mkPack(2, "Exp 2025-10", []uint64{15, 11, 14, 12, 13}) // M=5

	updated := time.Date(2025, 9, 4, 7, 0, 0, 0, time.UTC)
	urlFor := func(fid uint16) string { return "https://cdn.example.com/pack/" + itoa(fid) + ".json" }

	man, err := BuildManifest([]Pack{p1, p2, p3}, urlFor, 1, updated, 0.01) // include Bloom (~1% FPR)
	if err != nil {
		t.Fatalf("BuildManifest error: %v", err)
	}

	// schema + timestamp
	if man.SchemaVersion != 1 {
		t.Fatalf("schema_version mismatch: got %d want 1", man.SchemaVersion)
	}
	if man.UpdatedAt != updated.Format(time.RFC3339) {
		t.Fatalf("updated_at mismatch: got %s", man.UpdatedAt)
	}

	// Must have 3 packs sorted by M asc: p3(2) -> p1(3) -> p2(5)
	if len(man.Packs) != 3 {
		t.Fatalf("packs len=%d want=3", len(man.Packs))
	}
	gotFIDs := []uint16{man.Packs[0].FormatID, man.Packs[1].FormatID, man.Packs[2].FormatID}
	if !slices.Equal(gotFIDs, []uint16{3, 1, 2}) {
		t.Fatalf("order by M asc failed: got %v", gotFIDs)
	}

	// URL mapping
	if man.Packs[0].URL != "https://cdn.example.com/pack/3.json" ||
		man.Packs[1].URL != "https://cdn.example.com/pack/1.json" ||
		man.Packs[2].URL != "https://cdn.example.com/pack/2.json" {
		t.Fatalf("URL mapping mismatch: %+v", man.Packs)
	}

	// Bloom must exist and have no false negatives (members always "maybe")
	checkMembers := func(pm PackMeta, raw Pack) {
		if pm.Bloom == nil {
			t.Fatalf("missing bloom for fid=%d", pm.FormatID)
		}
		// BuildManifest sorted Pack.Cards internally; members should all be "maybe"
		slices.Sort(raw.Cards)
		for _, pk := range raw.Cards {
			if !pm.Bloom.MayContain(pk) {
				t.Fatalf("false negative for fid=%d pk=%d", pm.FormatID, pk)
			}
		}
	}
	checkMembers(man.Packs[0], p3)
	checkMembers(man.Packs[1], p1)
	checkMembers(man.Packs[2], p2)

	// JSON serialization sanity
	if _, err := json.Marshal(man); err != nil {
		t.Fatalf("manifest json marshal failed: %v", err)
	}
}

// TestBuildManifest_TieBreakByFormatID ensures packs with the same size (M)
// are ordered by FormatID ascending.
func TestBuildManifest_TieBreakByFormatID(t *testing.T) {
	// both have M=3
	pa := mkPack(10, "A", []uint64{3, 1, 2})
	pb := mkPack(2, "B", []uint64{6, 4, 5})
	urlFor := func(fid uint16) string { return "u" }
	man, err := BuildManifest([]Pack{pa, pb}, urlFor, 1, time.Now(), 0.01)
	if err != nil {
		t.Fatalf("BuildManifest error: %v", err)
	}
	// pb(FormatID=2) should come before pa(10)
	gotFIDs := []uint16{man.Packs[0].FormatID, man.Packs[1].FormatID}
	if !slices.Equal(gotFIDs, []uint16{2, 10}) {
		t.Fatalf("tie-break failed: got %v", gotFIDs)
	}
}

// TestBuildManifest_NoBloom asserts that when targetFP <= 0,
// no Bloom info is attached.
func TestBuildManifest_NoBloom(t *testing.T) {
	p1 := mkPack(1, "A", []uint64{1, 2, 3})
	p2 := mkPack(2, "B", []uint64{10, 20})
	urlFor := func(fid uint16) string { return "u" }

	man, err := BuildManifest([]Pack{p1, p2}, urlFor, 1, time.Now(), 0.0)
	if err != nil {
		t.Fatalf("BuildManifest error: %v", err)
	}
	for _, pm := range man.Packs {
		if pm.Bloom != nil {
			t.Fatalf("expected no bloom, got %+v", pm.Bloom)
		}
	}
}

// TestBuildManifest_Errors covers common error cases:
// - empty input
// - zero FormatID
// - duplicate FormatID
// - empty URL from callback
func TestBuildManifest_Errors(t *testing.T) {
	urlOK := func(fid uint16) string { return "u" }

	// Empty
	if _, err := BuildManifest(nil, urlOK, 1, time.Now(), 0.01); err == nil {
		t.Fatalf("expected error for empty packs, got nil")
	}

	// Zero FormatID
	pBad := mkPack(0, "bad", []uint64{1})
	if _, err := BuildManifest([]Pack{pBad}, urlOK, 1, time.Now(), 0.01); err == nil {
		t.Fatalf("expected error for zero FormatID, got nil")
	}

	// Duplicate FormatID
	pa := mkPack(7, "A", []uint64{1})
	pb := mkPack(7, "B", []uint64{2})
	if _, err := BuildManifest([]Pack{pa, pb}, urlOK, 1, time.Now(), 0.01); err == nil {
		t.Fatalf("expected error for duplicate format_id, got nil")
	}

	// Empty URL
	urlEmpty := func(fid uint16) string { return "" }
	if _, err := BuildManifest([]Pack{mkPack(9, "C", []uint64{1, 2})}, urlEmpty, 1, time.Now(), 0.01); err == nil {
		t.Fatalf("expected error for empty URL, got nil")
	}
}

//
// ------------------------- Bloom helper tests -------------------------
//

// TestBloom_NilOrEmpty asserts that a nil Bloom (or empty fields)
// behaves as "do-not-filter" (always returns true).
func TestBloom_NilOrEmpty(t *testing.T) {
	var b *BloomMeta
	if !b.MayContain(123) {
		t.Fatalf("nil bloom should return true")
	}
	nb := &BloomMeta{} // MBits=0, K=0
	if !nb.MayContain(456) {
		t.Fatalf("empty bloom should return true")
	}
	// Corrupted BitsB64 should also degrade to "true"
	nb2 := &BloomMeta{MBits: 64, K: 1, BitsB64: "!"} // invalid base64
	if !nb2.MayContain(789) {
		t.Fatalf("invalid bloom encoding should return true")
	}
}

// TestBloom_NoFalseNegatives verifies that buildBloomForCards produces a filter
// with no false negatives for the inserted set.
func TestBloom_NoFalseNegatives(t *testing.T) {
	cards := []uint64{10, 11, 12, 13, 14, 15}
	slices.Sort(cards)
	bl, err := buildBloomForCards(cards, 0.01)
	if err != nil {
		t.Fatalf("buildBloomForCards error: %v", err)
	}
	for _, pk := range cards {
		if !bl.MayContain(pk) {
			t.Fatalf("false negative: pk=%d", pk)
		}
	}
	// Sanity: Non-members are often filtered out (not guaranteed).
	outsiders := []uint64{1000, 1001, 1002, 1003, 1004}
	neg := 0
	for _, v := range outsiders {
		if !bl.MayContain(v) {
			neg++
		}
	}
	if neg == 0 {
		t.Logf("warning: all outsiders passed bloom (possible with small sample/FPR)")
	}
}

// TestOptimalBloomParamsMonotonic ensures that as n increases (for fixed p),
// mBits does not decrease and k >= 1.
func TestOptimalBloomParamsMonotonic(t *testing.T) {
	prevM := uint32(0)
	for _, n := range []int{1, 5, 10, 50, 100, 500, 1000} {
		m, k := optimalBloomParams(n, 0.01)
		if m < prevM {
			t.Fatalf("mBits decreased: n=%d m=%d prev=%d", n, m, prevM)
		}
		if k == 0 {
			t.Fatalf("k should be >=1 for n=%d", n)
		}
		prevM = m
	}
}

// TestFNV64WithSalt_Deterministic shows that hashing is deterministic and
// changes when pk or salt changes (practically guaranteed with FNV-1a).
func TestFNV64WithSalt_Deterministic(t *testing.T) {
	pk := uint64(123456789)
	s1 := uint64(0x1111111111111111)
	s2 := uint64(0x2222222222222222)

	h11 := fnv64WithSalt(pk, s1)
	h12 := fnv64WithSalt(pk, s1)
	if h11 != h12 {
		t.Fatalf("non-deterministic hash: %x vs %x", h11, h12)
	}
	h21 := fnv64WithSalt(pk, s2)
	if h21 == h11 {
		t.Fatalf("different salt should change hash")
	}
	h31 := fnv64WithSalt(pk+1, s1)
	if h31 == h11 {
		t.Fatalf("different pk should change hash")
	}
}
