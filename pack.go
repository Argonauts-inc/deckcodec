package deckcodec

import (
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"errors"
	"hash/fnv"
	"io"
	"math"
	"slices"
	"time"
)

// Pack represents a dictionary of card primary keys for a given format.
// Cards MUST be ascending for ordinal-based encoding to be stable.
type Pack struct {
	FormatID      uint16   `json:"format_id"`
	Name          string   `json:"name,omitempty"`
	CreatedAt     string   `json:"created_at,omitempty"`
	SchemaVersion int      `json:"schema_version,omitempty"`
	Cards         []uint64 `json:"cards"`
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

// ParsePack reads a Pack from any io.Reader (file, HTTP, memory buffer).
// It validates FormatID and sorts Cards ascending for stable ordinals.
func ParsePack(r io.Reader) (Pack, error) {
	var p Pack
	dec := json.NewDecoder(r)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&p); err != nil {
		return Pack{}, err
	}
	if p.FormatID == 0 {
		return Pack{}, errorsNew("deckcodec: pack.FormatID must be non-zero")
	}
	// Safety: keep cards ascending
	slices.Sort(p.Cards)
	return p, nil
}

// Manifest is the public index of all available packs.
// Host this on a CDN; keep packs immutable.
type Manifest struct {
	SchemaVersion int        `json:"schema_version"`
	UpdatedAt     string     `json:"updated_at,omitempty"` // RFC3339
	Packs         []PackMeta `json:"packs"`
}

// BloomMeta holds a compact membership filter for a pack.
// This lets clients pre-filter packs BEFORE downloading full pack JSON.
type BloomMeta struct {
	MBits   uint32 `json:"m_bits"`   // bit array length (m)
	K       uint8  `json:"k"`        // number of hash functions (k)
	Salt1   uint64 `json:"salt1"`    // double-hashing salt 1
	Salt2   uint64 `json:"salt2"`    // double-hashing salt 2
	BitsB64 string `json:"bits_b64"` // raw bits (little-endian bit order), Base64URL (no padding)
}

// MayContain returns true if the set *may contain* pk (Bloom semantics).
// If Bloom is nil/empty/unreadable, returns true (do-not-filter).
func (b *BloomMeta) MayContain(pk uint64) bool {
	if b == nil || b.MBits == 0 || b.K == 0 || b.BitsB64 == "" {
		return true
	}
	bits, err := base64.RawURLEncoding.DecodeString(b.BitsB64)
	if err != nil || uint32(len(bits)) != (b.MBits+7)/8 {
		return true
	}
	h1 := fnv64WithSalt(pk, b.Salt1)
	h2 := fnv64WithSalt(pk, b.Salt2)

	hasBit := func(i uint32) bool {
		byteIdx := i >> 3
		bitIdx := i & 7
		return (bits[byteIdx]>>bitIdx)&1 == 1
	}

	for i := uint32(0); i < uint32(b.K); i++ {
		idx := (h1 + uint64(i)*h2) % uint64(b.MBits)
		if !hasBit(uint32(idx)) {
			return false
		}
	}
	return true
}

// PackMeta summarizes one pack for the manifest.
// Bloom is optional and present only if targetFP > 0 when building the manifest.
type PackMeta struct {
	FormatID uint16     `json:"format_id"`
	Name     string     `json:"name,omitempty"`
	URL      string     `json:"url"` // absolute or CDN path to pack JSON
	M        int        `json:"M"`   // number of cards in the pack
	Bloom    *BloomMeta `json:"bloom,omitempty"`
}

// BuildManifest builds a manifest. If targetFP > 0, it attaches a Bloom filter
// to each PackMeta for pre-filtering (false-positive rate ~= targetFP).
//
// - packs are sorted ascending by M (tie-break by FormatID) for encode-side heuristics
// - urlFor(fid) must return a non-empty URL for each pack
// - duplicate or zero FormatID is rejected
func BuildManifest(
	packs []Pack,
	urlFor func(fid uint16) string,
	schemaVersion int,
	updatedAt time.Time,
	targetFP float64,
) (Manifest, error) {
	if len(packs) == 0 {
		return Manifest{}, errorsNew("deckcodec: no packs to build manifest")
	}
	seen := make(map[uint16]struct{}, len(packs))
	metas := make([]PackMeta, 0, len(packs))

	for _, p := range packs {
		if p.FormatID == 0 {
			return Manifest{}, errorsNew("deckcodec: pack.FormatID must be non-zero")
		}
		if _, dup := seen[p.FormatID]; dup {
			return Manifest{}, errorsNew("deckcodec: duplicate format_id in packs")
		}
		seen[p.FormatID] = struct{}{}

		// Safety: ensure ascending card order
		slices.Sort(p.Cards)

		u := ""
		if urlFor != nil {
			u = urlFor(p.FormatID)
		}
		if u == "" {
			return Manifest{}, errorsNew("deckcodec: urlFor returned empty URL")
		}

		pm := PackMeta{
			FormatID: p.FormatID,
			Name:     p.Name,
			URL:      u,
			M:        len(p.Cards),
		}

		// Optional Bloom
		if targetFP > 0 {
			bl, err := buildBloomForCards(p.Cards, targetFP)
			if err != nil {
				return Manifest{}, err
			}
			pm.Bloom = bl
		}

		metas = append(metas, pm)
	}

	// Sort small→large (encode-side tries smaller packs first)
	slices.SortFunc(metas, func(a, b PackMeta) int {
		if a.M != b.M {
			if a.M < b.M {
				return -1
			}
			return 1
		}
		if a.FormatID < b.FormatID {
			return -1
		}
		if a.FormatID > b.FormatID {
			return 1
		}
		return 0
	})

	return Manifest{
		SchemaVersion: schemaVersion,
		UpdatedAt:     updatedAt.UTC().Format(time.RFC3339),
		Packs:         metas,
	}, nil
}

// --- internal helpers ---

// tiny local error helper (to avoid importing "errors" here)
func errorsNew(s string) error { return &simpleError{s: s} }

type simpleError struct{ s string }

func (e *simpleError) Error() string { return e.s }

// buildBloomForCards constructs a Bloom filter for the given card PKs.
func buildBloomForCards(cards []uint64, targetFP float64) (*BloomMeta, error) {
	n := len(cards)
	if n == 0 || targetFP <= 0 || targetFP >= 1 {
		return &BloomMeta{MBits: 0, K: 0, BitsB64: ""}, nil
	}
	mBits, k := optimalBloomParams(n, targetFP)
	if mBits == 0 || k == 0 {
		return &BloomMeta{MBits: 0, K: 0, BitsB64: ""}, nil
	}

	// Deterministic salts (per manifest build)
	const salt1 = 0x9e3779b97f4a7c15
	const salt2 = 0xbf58476d1ce4e5b9

	bitBytes := (mBits + 7) / 8
	bits := make([]byte, bitBytes)

	setBit := func(i uint32) {
		byteIdx := i >> 3
		bitIdx := i & 7
		bits[byteIdx] |= 1 << bitIdx
	}

	for _, pk := range cards {
		h1 := fnv64WithSalt(pk, salt1)
		h2 := fnv64WithSalt(pk, salt2)
		for i := uint32(0); i < uint32(k); i++ {
			// double hashing: h(i) = h1 + i*h2
			idx := (h1 + uint64(i)*h2) % uint64(mBits)
			setBit(uint32(idx))
		}
	}

	return &BloomMeta{
		MBits:   mBits,
		K:       k,
		Salt1:   salt1,
		Salt2:   salt2,
		BitsB64: base64.RawURLEncoding.EncodeToString(bits),
	}, nil
}

// optimalBloomParams computes m (bits) and k (hash functions) for n items and target FP p.
// m = - (n * ln p) / (ln 2)^2 ; k = (m/n) * ln 2
// m is rounded up to a multiple of 64 for byte-aligned storage.
func optimalBloomParams(n int, p float64) (uint32, uint8) {
	if n <= 0 || p <= 0 || p >= 1 {
		return 0, 0
	}
	ln2 := math.Ln2
	m := -float64(n) * math.Log(p) / (ln2 * ln2)
	mBits := uint32(math.Ceil(m))
	// align to 64
	if rem := mBits % 64; rem != 0 {
		mBits += 64 - rem
	}
	k := uint8(math.Max(1, math.Round((float64(mBits)/float64(n))*ln2)))
	return mBits, k
}

// fnv64WithSalt hashes (pk || salt) with FNV-1a 64-bit.
func fnv64WithSalt(pk uint64, salt uint64) uint64 {
	h := fnv.New64a()
	var buf [16]byte
	binary.LittleEndian.PutUint64(buf[0:8], pk)
	binary.LittleEndian.PutUint64(buf[8:16], salt)
	_, _ = h.Write(buf[:])
	return h.Sum64()
}
