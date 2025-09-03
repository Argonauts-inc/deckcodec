package deckcodec

import (
	"encoding/json"
	"errors"
	"os"
	"slices"
)

type Pack struct {
	FormatID      uint16   `json:"format_id"`
	Name          string   `json:"name,omitempty"`
	CreatedAt     string   `json:"created_at,omitempty"`
	SchemaVersion int      `json:"schema_version,omitempty"`
	Cards         []uint64 `json:"cards"`
}

func LoadPack(path string) (*Pack, error) {
	// Create a variable to hold the decoded pack data
	var pack Pack

	// Read the entire file at the given path into memory
	b, err := os.ReadFile(path)
	if err != nil {
		// Return an error if the file could not be read
		return nil, err
	}

	// Unmarshal the JSON data from the file into the pack struct
	if err := json.Unmarshal(b, &pack); err != nil {
		// Return an error if the JSON is invalid or does not match the struct
		return nil, err
	}

	// Sort the Cards slice in ascending order for consistency
	// This ensures that the cards are always in a predictable order
	slices.Sort(pack.Cards)

	// Return a pointer to the loaded pack and nil error
	return &pack, nil
}

type PackBuildOpts struct {
	FormatID    uint16
	Name        string
	Deduplicate bool // default: true; remove duplicate card ids
}

// BuildPack builds a Pack from an in-memory list of PKs.
// It sorts ascending and (optionally) de-duplicates.
func BuildPack(pks []uint64, opts PackBuildOpts) (Pack, error) {
	if opts.FormatID == 0 {
		return Pack{}, errors.New("deckcodec: FormatID must be non-zero")
	}
	if len(pks) == 0 {
		return Pack{}, errors.New("deckcodec: no card PKs provided")
	}
	// Defensive copy
	cards := slices.Clone(pks)
	// Sort ascending
	slices.Sort(cards)
	// De-duplicate (recommended for stable ordinals)
	if opts.Deduplicate {
		cards = dedupSorted(cards)
	}
	return Pack{
		FormatID: opts.FormatID,
		Name:     opts.Name,
		Cards:    cards,
	}, nil
}

func dedupSorted(a []uint64) []uint64 {
	if len(a) <= 1 {
		return a
	}
	out := a[:1]
	for i := 1; i < len(a); i++ {
		if a[i] != out[len(out)-1] {
			out = append(out, a[i])
		}
	}
	return out
}
