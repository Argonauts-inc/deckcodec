# deckcodec

This project compresses Trading Card Game (TCG) decks into short, URL-safe strings using three ideas:
- Dictionary / ordinal encoding
- Bit-packing
- Base64URL (unpadded)

For large catalogs, a Bloom-filter–augmented manifest lets clients pick the smallest dictionary (“pack”) that covers a deck before downloading any heavy JSON.

## Overview of the codec

1) Dictionary / ordinal encoding

Each format publishes a pack = sorted list of card primary keys (PKs).
Files: pack.go (Pack, ParsePack, BuildPack)

A card is referenced by its ordinal in the pack instead of its (up to 64-bit) PK:
$$
\mathrm{ord}(pk) ;=; \min {, i \mid \mathrm{Cards}[i] = pk ,}.
$$

Let $M = |\mathrm{Cards}|$. Then any ordinal fits in
$$
\mathrm{id_bits} ;=; \left\lceil \log_2 M \right\rceil
$$
bits. This removes entropy you don’t need and turns IDs into compact fixed-width integers.

2) Bit-packing

We write fields as bitfields into a byte buffer, flushing every 8 bits.
Files: internal/bitio.go (+ tests)

Sections are normalized (sorted) to canonicalize encoding:
	•	Leader: $L$ ordinals
	•	Tactics: $T$ ordinals
	•	Deck: $U$ pairs $(\text{ordinal}, \text{count})$ with $\text{count}\in[1..4]$

Given the rule “count max = 4”, counts fit in 2 bits.

Conceptual bit layout

```
header:
  format_id: 16 bits (little-endian)
  sizes:     small integers for L, T, U (implementation detail)

body:
  leaders:   L × id_bits
  tactics:   T × id_bits
  deck:
    keys:    U × id_bits
    counts:  U × 2 bits      // since count ∈ [1..4]
```

Files: encode.go / decode.go (deckcodec.Encode / deckcodec.Decode)
Bit I/O: internal/bitio.go (Writer.WriteBits, Reader.ReadBits)

3) URL-safe Base64

The byte buffer is emitted as Base64URL (unpadded), alphabet [A-Za-z0-9_-], which is safe for URL paths (no /, +, =, #, ?).
Tests assert URL-safety in encode_test.go.

### How short does it get?

Let
- $M$ = pack size (distinct cards in the format)
- $L$, $T$ = count of leader / tactics cards in the deck
- $U$ = unique deck entries (not total cards), each with count in $[1..4]$

Then payload bits (excluding a small header) are well approximated by
$$
B ;\approx; (L + T + U),\underbrace{\left\lceil \log_2 M \right\rceil}_{\mathrm{id_bits}} ;+; 2U.
$$

Rough Base64URL length (no padding) is
$$
\text{chars} ;\approx; \left\lceil \frac{B}{6} \right\rceil,
$$
since each Base64 character carries 6 bits. (Exact length comes from bytes: ceil(bytes/3)*4.)

Implications
- Smaller packs ⇒ smaller $\mathrm{id_bits}$ ⇒ shorter codes.
- Keeping $U$ small (dedup deck list) saves both $\mathrm{id_bits}\cdot U$ and $2U$.

### Why it’s correct (decodable)

1.	Pack immutability
The first 16 bits carry format_id. Decode uses it to fetch the exact pack used for encoding. Packs are immutable (append pack/2.json, don’t edit pack/1.json).
Files: pack.go (Pack, ParsePack), example: examples/with_manifest.
2.	Fixed-width ordinals
Using $\mathrm{id_bits}=\lceil\log_2 M\rceil$ against the same pack on decode guarantees perfect inversion (no prefix ambiguity).
3.	Explicit section sizes
The header includes $L$, $T$, $U$, so the decoder knows exact boundaries.
4.	Sorted canonical order
Leaders/Tactics are sorted before encoding and returned sorted at decode → deterministic, order-independent representation.
Files: helpers.go (UniqSortedPKsFromDeck), encode.go.
5.	Tests
- Bit-level round-trip: internal/bitio_test.go
- Public API determinism & errors: encode_test.go
- Pack building & assumptions: buildpack_test.go
- Manifest + Bloom: manifest_bloom_test.go
- SDK helpers: helpers_test.go

### Manifest with Bloom filter (scales to many packs)

To keep manifests useful at scale, each PackMeta can embed a Bloom filter summary of its card set (optional via targetFP when building).
- False-positive, no false-negative membership test:
$$
p ;\approx; \bigl(1 - e^{-k n / m}\bigr)^k,
$$
where $n$ = items (cards), $m$ = bit array size, $k$ = number of hash functions.
- Optimal parameters (used by the SDK):
$$
m ;=; -\frac{n \ln p}{(\ln 2)^2}, \qquad
k ;=; \frac{m}{n},\ln 2.
$$
(We round $m$ up to a multiple of 64 bits for alignment.)
Files: pack.go → optimalBloomParams, buildBloomForCards
- Double hashing to simulate $k$ hashes:
$$
h_i(x) ;=; h_1(x) + i\cdot h_2(x) \pmod{m}, \quad 0 \le i < k,
$$
with 64-bit FNV-1a + salts.
Files: pack.go → fnv64WithSalt, BloomMeta.MayContain
- Client algorithm (encode path)
Given a deck’s unique set $U$, iterate packs in ascending $M$ and quickly check:

```
if deckcodec.MayContainAll(packMeta.Bloom, uniqPKs) {
    // candidate: fetch this pack JSON and verify containment exactly
}
```

**Size intuition**
For $n=500$ and $p=1%$, $m \approx 9.6n \approx 4800$ bits ≈ 600 bytes per pack.
Even with 100 packs, manifest adds ~60 KB (gzip-friendly), far smaller than fetching all packs.

### Complexity
- Encode: $O(L + T + U)$ to map PK→ordinal and bit-pack fields.
- Decode: $O(L + T + U)$ to read fixed widths and map ordinal→PK.
- Space: Pack is $O(M)$; code stream is $O((L+T+U)\cdot \mathrm{id_bits} + 2U)$ bits.

### Implementation map

Public API
- Encode / Decode: encode.go
- BuildPack, ParsePack, BuildManifest (+ Bloom): pack.go
- UniqSortedPKsFromDeck, MayContainAll: helpers.go

Internals
- Bit I/O: internal/bitio.go (+ internal/bitio_test.go)
- Tests: encode_test.go, manifest_bloom_test.go, helpers_test.go, buildpack_test.go

Examples
- End-to-end with manifest & local files: examples/with_manifest/main.go

### Design choices & trade-offs

- Pack immutability is non-negotiable.
Changing pack/1.json after codes are issued can break decoding (different $M$ ⇒ different $\mathrm{id_bits}$; changed order ⇒ different ordinals). Always append pack/2.json, etc.
- Counts in $[1..4]$ = 2 bits.
If your rules change (e.g., max 10), only the deck-count field width changes; leaders/tactics widths remain based on $M$.
- Variable vs. fixed header sizes.
$L$, $T$, $U$ are tiny and encoded compactly; they don’t scale with $M$.
- No padding in Base64URL.
Keeps paths short and copy/paste-friendly; the decoder tolerates unpadded input.

If you want a formal “wire spec” appendix (bit-exact header field definitions), we can add one and pin it with a schema_version.
