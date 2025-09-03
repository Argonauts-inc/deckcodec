package deckcodec

import (
	"encoding/json"
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
