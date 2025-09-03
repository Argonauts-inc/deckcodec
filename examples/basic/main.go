// examples/basic/main.go
package main

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"slices"

	"github.com/Argonauts-inc/deckcodec"
)

func main() {
	// 1) Build a dictionary ("pack") in memory using the SDK.
	pks := make([]uint64, 0, 200)
	for i := range 200 {
		pks = append(pks, uint64(i+1))
	}
	packBuilt, err := deckcodec.BuildPack(pks, deckcodec.PackBuildOpts{
		FormatID:    1,
		Name:        "Standard 2025-09",
		Deduplicate: true,
	})
	if err != nil {
		log.Fatalf("BuildPack: %v", err)
	}

	// 2) Serialize the pack to JSON and write it to disk.
	//    In real apps this file could be CDN-hosted and versioned by format_id.
	if err := os.MkdirAll("pack", 0o755); err != nil {
		log.Fatalf("mkdir pack/: %v", err)
	}
	packPath := "pack/1.json"
	data, err := json.MarshalIndent(packBuilt, "", "  ")
	if err != nil {
		log.Fatalf("marshal pack: %v", err)
	}
	if err := os.WriteFile(packPath, data, 0o644); err != nil {
		log.Fatalf("write %s: %v", packPath, err)
	}

	// 3) Load the pack back from JSON (this is what production code usually does).
	packLoaded, err := deckcodec.LoadPack(packPath)
	if err != nil {
		log.Fatalf("LoadPack: %v", err)
	}
	// (optional) Sanity check: loaded == built (cards are sorted and equal).
	if !slices.Equal(packLoaded.Cards, packBuilt.Cards) || packLoaded.FormatID != packBuilt.FormatID {
		log.Fatalf("loaded pack does not match the built pack")
	}

	// 4) Prepare a deck to encode.
	in := deckcodec.DeckInput{
		// Order does not matter; encoder normalizes leader/tactics to ascending.
		Leader:  []uint64{1, 2, 3, 4},                         // 4 leaders
		Tactics: []uint64{100, 101, 102, 103, 104},            // 5 tactics
		Deck:    map[uint64]uint8{50: 4, 51: 3, 52: 2, 53: 1}, // unique deck cards with counts (1..4)
	}

	// 5) Encode → URL-safe string (Base64URL without padding).
	code, err := deckcodec.Encode(*packLoaded, in)
	if err != nil {
		log.Fatalf("Encode: %v", err)
	}
	fmt.Println("code:", code)

	// 6) Decode → back to structured data.
	out, err := deckcodec.Decode(*packLoaded, code)
	if err != nil {
		log.Fatalf("Decode: %v", err)
	}

	// 7) Print the decoded deck (leaders/tactics are normalized ascending).
	outJSON, _ := json.MarshalIndent(struct {
		FormatID uint16           `json:"format_id"`
		Leader   []uint64         `json:"leader"`
		Tactics  []uint64         `json:"tactics"`
		Deck     map[uint64]uint8 `json:"deck"`
	}{
		FormatID: out.FormatID,
		Leader:   out.Leader,
		Tactics:  out.Tactics,
		Deck:     out.Deck,
	}, "", "  ")
	fmt.Println(string(outJSON))

	// 8) Optional: verify round-trip.
	wantL, wantT := slices.Clone(in.Leader), slices.Clone(in.Tactics)
	slices.Sort(wantL)
	slices.Sort(wantT)
	if !slices.Equal(wantL, out.Leader) || !slices.Equal(wantT, out.Tactics) {
		log.Fatalf("normalized sections mismatch")
	}
	for pk, c := range in.Deck {
		if out.Deck[pk] != c {
			log.Fatalf("count mismatch on %d: got=%d want=%d", pk, out.Deck[pk], c)
		}
	}
	fmt.Println("OK: round-trip verified")
}
