# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

This is a Go library called `deckcodec` that provides a compact codec for encoding and decoding Xross Stars card game decks. The library uses bit-level compression to efficiently encode deck configurations into base64 strings.

## Architecture

The codebase has a clean modular structure:

- **encode.go**: Core encoding/decoding logic
  - `DeckInput`: Input structure with Leader cards, Tactics cards, and main Deck (card ID → count map)
  - `DeckOutput`: Output structure with FormatID and decoded deck data
  - `Encode()`: Converts deck to compact base64 string using bit-packing
  - `Decode()`: Reconstructs deck from base64 string

- **pack.go**: Pack file handling
  - `Pack`: Structure representing a card set with FormatID and sorted card list
  - `LoadPack()`: Loads pack definition from JSON file

- **internal/bitio.go**: Bit-level I/O utilities
  - `Writer`: Accumulates and writes bits to byte buffer
  - `Reader`: Reads arbitrary bit widths from byte stream
  - Used for efficient variable-width encoding of card ordinals

## Key Implementation Details

The codec uses a space-efficient format:
1. 16-bit format ID header
2. Variable-width card ordinals (log2(pack_size) bits per card)
3. 2-bit card counts (1-4 encoded as 0-3)
4. Deterministic encoding via sorted ordinals

## Common Development Commands

```bash
# Run all tests
go test ./...

# Run tests with verbose output
go test -v ./...

# Run tests for a specific package
go test ./internal

# Run a specific test function
go test -run TestEncodeDecode

# Check for compilation errors
go build ./...

# Format code
go fmt ./...

# Lint code (if golangci-lint is installed)
golangci-lint run
```

## Testing

The codebase includes comprehensive unit tests:
- `encode_test.go`: Tests for encoding/decoding logic
- `pack_test.go`: Tests for pack loading functionality  
- `internal/bitio_test.go`: Tests for bit I/O operations