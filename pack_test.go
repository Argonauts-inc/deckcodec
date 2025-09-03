package deckcodec

import (
	"os"
	"path/filepath"
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
