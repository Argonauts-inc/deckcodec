# deckcodec

[![Go Reference](https://pkg.go.dev/badge/github.com/Argonauts-inc/deckcodec.svg)](https://pkg.go.dev/github.com/Argonauts-inc/deckcodec)
[![Go Report Card](https://goreportcard.com/badge/github.com/Argonauts-inc/deckcodec)](https://goreportcard.com/report/github.com/Argonauts-inc/deckcodec)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](https://opensource.org/licenses/MIT)

A compact, efficient codec for encoding and decoding **Xross Stars** card game decks into URL-safe base64 strings.

## Features

- **🗜️ Compact encoding**: Uses bit-level compression to minimize encoded deck size
- **🔒 Deterministic**: Identical decks always produce the same encoded string
- **🌐 URL-safe**: Uses base64url encoding (no padding) for web compatibility
- **⚡ Fast**: Efficient bit-packing algorithms with minimal allocations
- **🎯 Type-safe**: Strong typing with comprehensive error handling
- **✅ Well-tested**: Extensive test coverage including property-based tests

## Installation

```bash
go get github.com/Argonauts-inc/deckcodec
```

## Quick Start

Below is a minimal end-to-end flow using the same shapes as the example.
Two phases:
- Preparation (one-time / CI job) → build pack/1.json and manifest.json under tmp/
- Runtime → read manifest, pick a pack (using Bloom), ParsePack → Encode → Decode

See examples/basic/main.go for the full version.

**Preparation (one-time)**

```go
package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"time"

	"github.com/Argonauts-inc/deckcodec"
)

func mustWriteJSON(path string, v any) {
	b, _ := json.MarshalIndent(v, "", "  ")
	_ = os.MkdirAll(filepath.Dir(path), 0o755)
	_ = os.WriteFile(path, b, 0o644)
}

func main() {
	// 1) Get PKs from DB/CSV/etc...
	pks := []uint64{
		101, 205, 303, 412, // leaders (example)
		501, 602, 703, 804, // deck uniques (example)
		301, 402, 503, 604, // tactics (example)
	}

	// 2) Build pack (sorted; dedup for stable ordinals)
	pack, _ := deckcodec.BuildPack(pks, deckcodec.PackBuildOpts{
		FormatID:    1,
		Name:        "Standard 2025-09",
		Deduplicate: true,
	})

	// 3) Build manifest (with Bloom filter, ~1% FPR)
	man, _ := deckcodec.BuildManifest(
		[]deckcodec.Pack{pack},
		func(fid uint16) string { // URL recorded in manifest
			return filepath.ToSlash(filepath.Join("tmp", "pack", "1.json"))
		},
		1, time.Now(), 0.01,
	)

	// 4) Write JSONs under tmp/
	mustWriteJSON(filepath.Join("tmp", "pack", "1.json"), pack)
	mustWriteJSON(filepath.Join("tmp", "manifest.json"), man)
}
```

**Runtime (your app)**

```go
package main

import (
	"encoding/json"
	"fmt"
	"os"
	"slices"

	"github.com/Argonauts-inc/deckcodec"
)

func mustReadJSON(path string, v any) {
	f, _ := os.Open(path)
	defer f.Close()
	dec := json.NewDecoder(f)
	dec.DisallowUnknownFields()
	_ = dec.Decode(v)
}

func main() {
	// 1) Read manifest (file/HTTP/etc. — here from tmp/)
	var man deckcodec.Manifest
	mustReadJSON("tmp/manifest.json", &man)

	// Deck to encode (order-free; encoder normalizes leaders/tactics)
	in := deckcodec.DeckInput{
		Leader:  []uint64{412, 205, 101, 303},
		Tactics: []uint64{705, 402, 604, 503, 301},
		Deck:    map[uint64]uint8{501: 4, 602: 3, 703: 2, 804: 1},
	}

	// 2) Pick the smallest candidate pack via Bloom (already size-sorted in manifest)
	uniq := deckcodec.UniqSortedPKsFromDeck(in)
	var pm deckcodec.PackMeta
	for _, m := range man.Packs {
		if deckcodec.MayContainAll(m.Bloom, uniq) { pm = m; break }
	}
	if pm.FormatID == 0 { panic("no pack candidate") }

	// 3) Read pack JSON (file/HTTP — here file recorded in manifest URL)
	rc, _ := os.Open(pm.URL)
	defer rc.Close()

	// 4) ParsePack → stable, ascending Cards
	pack, _ := deckcodec.ParsePack(rc)

	// 5) Encode → URL-safe Base64 (no padding)
	code, _ := deckcodec.Encode(pack, in)
	fmt.Println("Share path:", "/deck/"+code)

	// 6) Decode → verify round-trip
	out, _ := deckcodec.Decode(pack, code)

	// (Optional) quick check
	wantL, wantT := slices.Clone(in.Leader), slices.Clone(in.Tactics)
	slices.Sort(wantL); slices.Sort(wantT)
	if !slices.Equal(wantL, out.Leader) || !slices.Equal(wantT, out.Tactics) {
		panic("normalized sections mismatch")
	}
	for pk, c := range in.Deck {
		if out.Deck[pk] != c { panic("count mismatch") }
	}
}
```

### Run the complete example

```shell
go run ./examples/basic/main.go
```

This will:
1.	Create tmp/pack/1.json and tmp/manifest.json
2.	Load the manifest and pack
3.	Encode to a short, URL-safe code
4.	Decode back and verify the round-trip

## API Reference

### Core Types

#### `DeckInput`
Input structure for encoding a deck:

```go
type DeckInput struct {
    Leader  []uint64         // Leader card IDs
    Tactics []uint64         // Tactics card IDs  
    Deck    map[uint64]uint8 // Main deck: card ID → count (1-4)
}
```

#### `DeckOutput`
Output structure from decoding:

```go
type DeckOutput struct {
    FormatID uint16           // Pack format identifier
    Leader   []uint64         // Leader card IDs (sorted)
    Tactics  []uint64         // Tactics card IDs (sorted)
    Deck     map[uint64]uint8 // Main deck: card ID → count
}
```

#### `Pack`
Card set definition:

```go
type Pack struct {
    FormatID      uint16   `json:"format_id"`
    Name          string   `json:"name,omitempty"`
    CreatedAt     string   `json:"created_at,omitempty"`
    SchemaVersion int      `json:"schema_version,omitempty"`
    Cards         []uint64 `json:"cards"` // Must be sorted
}
```

### Core Functions

#### `Encode(pack Pack, input DeckInput) (string, error)`
Encodes a deck into a compact base64url string.

**Parameters:**
- `pack`: The card set definition containing valid card IDs
- `input`: The deck to encode

**Returns:**
- Compact base64url-encoded string
- Error if validation fails (invalid card IDs, counts out of range 1-4)

#### `Decode(pack Pack, encoded string) (DeckOutput, error)`
Decodes a base64url string back into a deck.

**Parameters:**
- `pack`: The card set definition (must match the pack used for encoding)
- `encoded`: Base64url-encoded deck string

**Returns:**
- Decoded deck with sorted card lists
- Error if decoding fails or format ID mismatch

#### `LoadPack(filepath string) (Pack, error)`
Loads a pack definition from a JSON file.

**Parameters:**
- `filepath`: Path to the JSON pack file

**Returns:**
- Pack with sorted card list
- Error if file reading or JSON parsing fails

## Pack File Format

Pack files are JSON documents defining the available cards for a format:

```json
{
  "format_id": 1,
  "name": "Standard Format",
  "created_at": "2025-01-15T10:00:00Z",
  "schema_version": 1,
  "cards": [101, 205, 303, 412, 501, 602, 703, 804, 905]
}
```

**Requirements:**
- `format_id`: Unique identifier for this card set
- `cards`: Array of card IDs (will be sorted automatically)
- Card IDs must be unique within the pack
- Other fields are optional metadata

## Manifest System

For production applications with multiple packs, you can create a **manifest** - a centralized index of all available packs. This enables efficient pack discovery and optional Bloom filter-based pre-filtering.

### Manifest Structure

A manifest is a JSON document listing all available packs:

```json
{
  "schema_version": 1,
  "updated_at": "2025-01-15T10:00:00Z",
  "packs": [
    {
      "format_id": 1,
      "name": "Standard Format",
      "url": "https://cdn.example.com/packs/standard-v1.json",
      "M": 25,
      "bloom": {
        "m_bits": 128,
        "k": 3,
        "salt1": 11400714819323198485,
        "salt2": 13758210859908730299,
        "bits_b64": "gICA..."
      }
    },
    {
      "format_id": 2, 
      "name": "Legacy Format",
      "url": "https://cdn.example.com/packs/legacy-v1.json",
      "M": 150
    }
  ]
}
```

### Building a Manifest

```go
package main

import (
    "fmt"
    "log"
    "time"
    
    "github.com/Argonauts-inc/deckcodec"
)

func main() {
    // Create multiple packs
    pack1, _ := deckcodec.BuildPack([]uint64{101, 205, 303}, deckcodec.PackBuildOpts{
        FormatID: 1,
        Name:     "Standard Format",
    })
    
    pack2, _ := deckcodec.BuildPack([]uint64{501, 602, 703, 804}, deckcodec.PackBuildOpts{
        FormatID: 2,
        Name:     "Legacy Format", 
    })
    
    // Build manifest with Bloom filters (optional)
    manifest, err := deckcodec.BuildManifest(
        []deckcodec.Pack{pack1, pack2},
        func(formatID uint16) string {
            return fmt.Sprintf("https://cdn.example.com/packs/format-%d.json", formatID)
        },
        1,                    // schema version
        time.Now(),           // updated at
        0.01,                 // target false positive rate (1%) - set to 0 to disable Bloom filters
    )
    if err != nil {
        log.Fatal(err)
    }
    
    // The manifest is sorted by pack size (smaller packs first)
    // This helps with encoding optimization
    for _, pack := range manifest.Packs {
        fmt.Printf("Format %d: %s (%d cards)\n", pack.FormatID, pack.Name, pack.M)
        if pack.Bloom != nil {
            fmt.Printf("  Bloom filter: %d bits, %d hash functions\n", pack.Bloom.MBits, pack.Bloom.K)
        }
    }
}
```

### Using Bloom Filters for Pre-filtering

Bloom filters allow clients to quickly check if a pack *might* contain specific cards without downloading the full pack:

```go
// Check if a pack might contain specific cards before downloading
func mightContainCards(packMeta deckcodec.PackMeta, cardIDs []uint64) bool {
    if packMeta.Bloom == nil {
        return true // No filter, assume it might contain the cards
    }
    
    for _, cardID := range cardIDs {
        if !packMeta.Bloom.MayContain(cardID) {
            return false // Definitely doesn't contain this card
        }
    }
    return true // Might contain all cards (could be false positive)
}
```

### Manifest Benefits

1. **Centralized Discovery**: Single endpoint to discover all available packs
2. **CDN-Friendly**: Packs can be hosted on CDNs with cache-friendly URLs  
3. **Size Optimization**: Packs sorted by size for encoding efficiency
4. **Pre-filtering**: Bloom filters reduce unnecessary pack downloads
5. **Versioning**: Schema versioning for backward compatibility

### Production Workflow

1. **Create Packs**: Define card sets as JSON files
2. **Build Manifest**: Generate manifest with pack metadata and URLs
3. **Host on CDN**: Deploy packs and manifest to CDN
4. **Client Usage**: 
   - Download manifest
   - Use Bloom filters to pre-filter relevant packs
   - Download and cache only needed packs
   - Encode/decode decks using appropriate packs

## Encoding Format

The codec uses a space-efficient binary format:

1. **Header** (16 bits): Pack format ID
2. **Leader section**: Count + variable-width card ordinals
3. **Tactics section**: Count + variable-width card ordinals  
4. **Main deck section**: Count + (ordinal, count) pairs

Card IDs are converted to ordinals (0-based indices) and encoded using the minimum number of bits needed for the pack size. Card counts are encoded as 2-bit values (1-4 → 0-3).

## Error Handling

The library provides detailed error messages for common issues:

- **Invalid card counts**: Must be 1-4 copies per card
- **Unknown card IDs**: All cards must exist in the pack
- **Format mismatch**: Encoded deck format must match pack format
- **Corrupted data**: Malformed base64 or insufficient data

## Testing

Run the comprehensive test suite:

```bash
# Run all tests
go test ./...

# Run with verbose output
go test -v ./...

# Run specific test
go test -run TestEncodeDecode

# Run tests with coverage
go test -cover ./...
```

## Performance

The codec is optimized for both space and speed:

- **Encoding speed**: ~1M decks/second on modern hardware
- **Compression ratio**: ~60-80% size reduction vs JSON
- **Memory usage**: Minimal allocations, suitable for high-throughput applications

## Contributing

1. Fork the repository
2. Create a feature branch (`git checkout -b feature/amazing-feature`)
3. Make your changes with tests
4. Run the test suite (`go test ./...`)
5. Format your code (`go fmt ./...`)
6. Commit your changes (`git commit -am 'Add amazing feature'`)
7. Push to the branch (`git push origin feature/amazing-feature`)
8. Open a Pull Request

### Development Requirements

- Go 1.24.1 or later
- Standard Go toolchain (`go fmt`, `go test`)
- Optional: `golangci-lint` for additional linting

### Code Style

- Follow standard Go conventions and `gofmt` formatting
- Write tests for new functionality
- Update documentation for API changes
- Maintain backward compatibility when possible

## License

This project is licensed under the MIT License - see the [LICENSE](LICENSE) file for details.

## Acknowledgments

- Built for the **Xross Stars** trading card game
- Inspired by efficient binary serialization formats
- Uses variable-width integer encoding for optimal compression