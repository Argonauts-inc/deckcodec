package deckcodec

import (
	"math"
	"os"
	"path/filepath"
	"slices"
	"testing"
)

// writeTempFile writes a file under t.TempDir() and returns its path.
func writeTempFile(t *testing.T, name, contents string) string {
	t.Helper()
	dir := t.TempDir()
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, []byte(contents), 0o644); err != nil {
		t.Fatalf("write temp file: %v", err)
	}
	return p
}

// TestLoadPack_SortsCards verifies that LoadPack sorts the Cards slice ascending,
// even when the source JSON is unsorted (and with duplicates).
func TestLoadPack_SortsCards(t *testing.T) {
	path := writeTempFile(t, "pack.json", `{
		"format_id": 1,
		"cards": [5,3,7,3,2,9]
	}`)

	p, err := LoadPack(path)
	if err != nil {
		t.Fatalf("LoadPack returned error: %v", err)
	}

	got := p.Cards
	want := []uint64{2, 3, 3, 5, 7, 9}

	if len(got) != len(want) {
		t.Fatalf("length mismatch: got %d want %d", len(got), len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("sorted order mismatch at %d: got %v want %v", i, got, want)
		}
	}
}

// TestLoadPack_InvalidJSON ensures that invalid JSON is reported as an error.
func TestLoadPack_InvalidJSON(t *testing.T) {
	path := writeTempFile(t, "bad.json", `{"format_id":1,"cards":[1,2,]}`) // trailing comma

	if _, err := LoadPack(path); err == nil {
		t.Fatalf("expected error for invalid JSON, got nil")
	}
}

// TestLoadPack_NotFound ensures that a missing file is reported as an error.
func TestLoadPack_NotFound(t *testing.T) {
	path := filepath.Join(t.TempDir(), "missing.json")
	if _, err := LoadPack(path); err == nil {
		t.Fatalf("expected error for missing file, got nil")
	}
}

// TestBuildPack_SortsAndDedups verifies that BuildPack sorts ascending and
// removes duplicates when Deduplicate=true, and that meta fields are preserved.
func TestBuildPack_SortsAndDedups(t *testing.T) {
	pks := []uint64{5, 3, 7, 3, 2, 9, 2}
	opts := PackBuildOpts{
		FormatID:    42,
		Name:        "Std 2025-09",
		Deduplicate: true,
	}

	p, err := BuildPack(pks, opts)
	if err != nil {
		t.Fatalf("BuildPack returned error: %v", err)
	}

	// Expect ascending unique list.
	want := []uint64{2, 3, 5, 7, 9}
	if !slices.Equal(p.Cards, want) {
		t.Fatalf("cards mismatch:\n  got:  %v\n  want: %v", p.Cards, want)
	}
	if !slices.IsSorted(p.Cards) {
		t.Fatalf("cards not sorted: %v", p.Cards)
	}
	if p.FormatID != 42 || p.Name != "Std 2025-09" {
		t.Fatalf("meta fields mismatch: FormatID=%d Name=%q", p.FormatID, p.Name)
	}
}

// TestBuildPack_NoDedup_KeepsDuplicates verifies that when Deduplicate=false
// the result is still sorted but duplicates are retained.
func TestBuildPack_NoDedup_KeepsDuplicates(t *testing.T) {
	pks := []uint64{5, 3, 7, 3, 2, 9, 2}
	opts := PackBuildOpts{
		FormatID:    1,
		Name:        "NoDedup",
		Deduplicate: false,
	}

	p, err := BuildPack(pks, opts)
	if err != nil {
		t.Fatalf("BuildPack returned error: %v", err)
	}

	// Sorted but duplicates preserved.
	want := []uint64{2, 2, 3, 3, 5, 7, 9}
	if !slices.Equal(p.Cards, want) {
		t.Fatalf("cards mismatch:\n  got:  %v\n  want: %v", p.Cards, want)
	}
	if !slices.IsSorted(p.Cards) {
		t.Fatalf("cards not sorted: %v", p.Cards)
	}
}

// TestBuildPack_DoesNotMutateInput ensures the input slice is not modified
// (the function clones the slice before sorting/deduplicating).
func TestBuildPack_DoesNotMutateInput(t *testing.T) {
	orig := []uint64{10, 1, 10, 3}
	in := slices.Clone(orig)

	_, err := BuildPack(in, PackBuildOpts{FormatID: 7, Deduplicate: true})
	if err != nil {
		t.Fatalf("BuildPack returned error: %v", err)
	}
	// Input must remain exactly as it was.
	if !slices.Equal(in, orig) {
		t.Fatalf("input slice mutated:\n  got:  %v\n  want: %v", in, orig)
	}
}

// TestBuildPack_LargeValues demonstrates handling of large uint64 values (including MaxUint64).
func TestBuildPack_LargeValues(t *testing.T) {
	pks := []uint64{math.MaxUint64, 1, 42, 9999999999999999999}
	opts := PackBuildOpts{FormatID: 9, Deduplicate: true}

	p, err := BuildPack(pks, opts)
	if err != nil {
		t.Fatalf("BuildPack returned error: %v", err)
	}

	// Expect ascending order with all items preserved (no duplicates here).
	want := slices.Clone(pks)
	slices.Sort(want)
	if !slices.Equal(p.Cards, want) {
		t.Fatalf("cards mismatch:\n  got:  %v\n  want: %v", p.Cards, want)
	}
}

// TestBuildPack_Errors validates error cases: empty PKs and zero FormatID.
func TestBuildPack_Errors(t *testing.T) {
	// Empty PK list
	if _, err := BuildPack(nil, PackBuildOpts{FormatID: 1}); err == nil {
		t.Fatalf("expected error for empty PKs, got nil")
	}
	if _, err := BuildPack([]uint64{}, PackBuildOpts{FormatID: 1}); err == nil {
		t.Fatalf("expected error for empty PKs, got nil")
	}

	// FormatID must be non-zero
	if _, err := BuildPack([]uint64{1}, PackBuildOpts{FormatID: 0}); err == nil {
		t.Fatalf("expected error for zero FormatID, got nil")
	}
}

// TestBuildPack_Idempotency shows that running BuildPack on an already sorted
// (and deduplicated) list is effectively a no-op (result is identical).
func TestBuildPack_Idempotency(t *testing.T) {
	sortedUnique := []uint64{2, 3, 5, 7, 11, 13}
	opts := PackBuildOpts{FormatID: 100, Deduplicate: true}

	p, err := BuildPack(sortedUnique, opts)
	if err != nil {
		t.Fatalf("BuildPack returned error: %v", err)
	}
	if !slices.Equal(p.Cards, sortedUnique) || !slices.IsSorted(p.Cards) {
		t.Fatalf("idempotency failed:\n  got:  %v\n  want: %v", p.Cards, sortedUnique)
	}
}
