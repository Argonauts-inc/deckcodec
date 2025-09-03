// examples/with_manifest/main.go
package main

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/Argonauts-inc/deckcodec"
)

/*
Preparation (one-time or CI job)
  1) Get PKs from DB / CSV / etc...
  2) Build Pack (sorted, dedup for stable ordinals)
  3) Build Manifest (optionally with Bloom filters)
  4) Write JSON files: tmp/pack/1.json and tmp/manifest.json

Implementation (runtime in your app)
  1) Read manifest (from file, HTTP, etc.)
  2) Select a pack (optionally pre-filter with Bloom)
  3) Read pack JSON (file/HTTP)
  4) deckcodec.ParsePack(io.Reader)
  5) deckcodec.Encode(pack, deck)
  6) deckcodec.Decode(pack, code) and verify
*/

func main() {
	// ---------------------------
	// Preparation (demo only)
	// ---------------------------
	// Create output directories under tmp/
	if err := os.MkdirAll(filepath.Join("tmp", "pack"), 0o755); err != nil {
		log.Fatalf("mkdir tmp/pack: %v", err)
	}

	// 1) Example PKs (would come from DB/CSV in real life)
	pks := []uint64{
		101, 205, 303, 412, // leaders
		501, 602, 703, 804, 905, // deck uniques
		1006, 1107, 1208, 1309, 1410, 1511, 1612, 1713, 1814, 1915,
		2016, 2117,
		301, 402, 503, 604, 705, // tactics
	}

	// 2) Build Pack
	pack, err := deckcodec.BuildPack(pks, deckcodec.PackBuildOpts{
		FormatID:    1,
		Name:        "Standard 2025-09",
		Deduplicate: true, // ensure stable ordinals
	})
	if err != nil {
		log.Fatalf("BuildPack: %v", err)
	}

	// 3) Build Manifest (with Bloom filter target false-positive rate ~1%)
	man, err := deckcodec.BuildManifest(
		[]deckcodec.Pack{pack},
		func(fid uint16) string {
			// URL/Path stored in manifest; here we point to local tmp/pack/<fid>.json
			return filepath.ToSlash(filepath.Join("tmp", "pack", fmtInt(fid)+".json"))
		},
		1, time.Now(), 0.01,
	)
	if err != nil {
		log.Fatalf("BuildManifest: %v", err)
	}

	// 4) Write JSON (pack + manifest) under tmp/
	packPath := filepath.Join("tmp", "pack", "1.json")
	writeJSON(packPath, pack)
	writeJSON(filepath.Join("tmp", "manifest.json"), man)

	// ---------------------------
	// Implementation (runtime)
	// ---------------------------

	// 1) Read manifest (from tmp/)
	var loadedMan deckcodec.Manifest
	readJSON(filepath.Join("tmp", "manifest.json"), &loadedMan)

	// Prepare a deck to encode
	in := deckcodec.DeckInput{
		Leader:  []uint64{412, 205, 101, 303},                     // 4 leaders (order free)
		Tactics: []uint64{705, 402, 604, 503, 301},                // 5 tactics (order free)
		Deck:    map[uint64]uint8{501: 4, 602: 3, 703: 2, 804: 1}, // counts 1..4
	}
	uniq := deckcodec.UniqSortedPKsFromDeck(in)

	// 2) Select a pack (prefer smaller first; pre-filter with Bloom where present)
	var chosen deckcodec.PackMeta
	for _, pm := range loadedMan.Packs { // already sorted by size asc
		if deckcodec.MayContainAll(pm.Bloom, uniq) {
			chosen = pm
			break
		}
	}
	if chosen.FormatID == 0 {
		log.Fatalf("no candidate pack in manifest")
	}

	// 3) Read pack JSON (support both file path or HTTP URL; here local file path in tmp/)
	rc, err := fetch(chosen.URL)
	if err != nil {
		log.Fatalf("open pack URL %q: %v", chosen.URL, err)
	}
	defer rc.Close()

	// 4) ParsePack
	p2, err := deckcodec.ParsePack(rc)
	if err != nil {
		log.Fatalf("ParsePack: %v", err)
	}

	// 5) Encode → URL-safe string
	code, err := deckcodec.Encode(p2, in)
	if err != nil {
		log.Fatalf("Encode: %v", err)
	}
	fmt.Println("code:", code)
	fmt.Println("code length:", len(code))

	// 6) Decode → verify round-trip
	out, err := deckcodec.Decode(p2, code)
	if err != nil {
		log.Fatalf("Decode: %v", err)
	}

	// Pretty print decoded deck
	prettyPrint(out)

	// Verify normalized sections + counts
	wantL := slices.Clone(in.Leader)
	wantT := slices.Clone(in.Tactics)
	slices.Sort(wantL)
	slices.Sort(wantT)
	if !slices.Equal(wantL, out.Leader) || !slices.Equal(wantT, out.Tactics) {
		log.Fatalf("normalized mismatch (leaders/tactics)")
	}
	for pk, c := range in.Deck {
		if out.Deck[pk] != c {
			log.Fatalf("count mismatch on pk=%d: got=%d want=%d", pk, out.Deck[pk], c)
		}
	}
	fmt.Println("OK: encode/decode round-trip verified")
}

// ---------------------------
// Helpers (example-only)
// ---------------------------

// writeJSON writes a value to path with pretty JSON.
func writeJSON(path string, v any) {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		log.Fatalf("marshal %s: %v", path, err)
	}
	if err := os.WriteFile(path, b, 0o644); err != nil {
		log.Fatalf("write %s: %v", path, err)
	}
}

// readJSON reads JSON from path into v.
func readJSON(path string, v any) {
	f, err := os.Open(path)
	if err != nil {
		log.Fatalf("open %s: %v", path, err)
	}
	defer f.Close()
	dec := json.NewDecoder(f)
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		log.Fatalf("decode %s: %v", path, err)
	}
}

// fetch returns an io.ReadCloser for a URL or local file path.
// If url starts with http/https, it does HTTP GET; otherwise opens file.
func fetch(url string) (io.ReadCloser, error) {
	if strings.HasPrefix(url, "http://") || strings.HasPrefix(url, "https://") {
		resp, err := http.Get(url) // #nosec G107 (example code)
		if err != nil {
			return nil, err
		}
		if resp.StatusCode/100 != 2 {
			defer resp.Body.Close()
			return nil, fmt.Errorf("HTTP %d for %s", resp.StatusCode, url)
		}
		return resp.Body, nil
	}
	return os.Open(url)
}

// prettyPrint prints the decoded deck as JSON.
func prettyPrint(out deckcodec.DeckOutput) {
	j, _ := json.MarshalIndent(struct {
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
	fmt.Println(string(j))
}

// fmtInt is a tiny helper for format_id → "1"
func fmtInt(fid uint16) string {
	n := int(fid)
	if n == 0 {
		return "0"
	}
	var buf [6]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + (n % 10))
		n /= 10
	}
	return string(buf[i:])
}
