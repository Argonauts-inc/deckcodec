package bitio

import (
	"errors"
	"math/rand"
	"testing"
	"time"
)

// mask returns a value with the lowest 'w' bits set to 1.
func mask(w int) uint32 {
	if w <= 0 {
		return 0
	}
	return (1 << w) - 1
}

// bitsToBytes rounds up the bit length to whole bytes.
func bitsToBytes(bits int) int {
	return (bits + 7) / 8
}

// TestRoundTripFixedWidths verifies that a sequence of (value,width) pairs
// written with Writer can be read back with the exact same widths using Reader.
// This also stresses cross-byte boundaries (e.g., 3+5+9 bits etc.).
func TestRoundTripFixedWidths(t *testing.T) {
	type pair struct {
		w int
		v uint32
	}
	seq := []pair{
		{1, 1},     // 1 bit
		{2, 2},     // 10b
		{3, 5},     // 101b
		{5, 0x1F},  // 11111b
		{7, 0x55},  // 1010101b
		{8, 0xA5},  // full byte
		{9, 0x1AB}, // crosses multiple bytes
		{13, 0x1234 & mask(13)},
		{16, 0xBEEF},
	}

	// Write
	var w Writer
	totalBits := 0
	for _, p := range seq {
		w.WriteBits(p.v, p.w)
		totalBits += p.w
	}
	buf := w.Finish()

	// Sanity: buffer length should match ceil(totalBits/8).
	if got, want := len(buf), bitsToBytes(totalBits); got != want {
		t.Fatalf("buffer length mismatch: got %d, want %d (totalBits=%d)", got, want, totalBits)
	}

	// Read back
	r := NewReader(buf, totalBits)
	for i, p := range seq {
		got, err := r.ReadBits(p.w)
		if err != nil {
			t.Fatalf("ReadBits failed at step %d: %v", i, err)
		}
		want := p.v & mask(p.w) // high bits beyond width must be ignored
		if got != want {
			t.Fatalf("mismatch at step %d: got=0x%X want=0x%X (width=%d)",
				i, got, want, p.w)
		}
	}
}

// TestFlushBehavior checks that Finish flushes a trailing partial byte exactly once
// and does not add extra bytes when the bit count is already byte-aligned.
func TestFlushBehavior(t *testing.T) {
	// Case 1: not byte-aligned (13 bits)
	var w1 Writer
	w1.WriteBits(0x1FFF, 13)
	buf1 := w1.Finish()
	if len(buf1) != bitsToBytes(13) {
		t.Fatalf("not byte-aligned: got %d bytes, want %d", len(buf1), bitsToBytes(13))
	}
	// Round-trip guarded
	r1 := NewReader(buf1, 13)
	if v, err := r1.ReadBits(13); err != nil || v != (0x1FFF&mask(13)) {
		t.Fatalf("round-trip failed for 13 bits: v=0x%X err=%v", v, err)
	}

	// Case 2: already byte-aligned (16 bits)
	var w2 Writer
	w2.WriteBits(0xABCD, 16)
	buf2 := w2.Finish()
	if len(buf2) != bitsToBytes(16) {
		t.Fatalf("byte-aligned: got %d bytes, want %d", len(buf2), bitsToBytes(16))
	}
	r2 := NewReader(buf2, 16)
	if v, err := r2.ReadBits(16); err != nil || v != 0xABCD {
		t.Fatalf("round-trip failed for 16 bits: v=0x%X err=%v", v, err)
	}
}

// TestErrShort ensures Reader returns ErrShort when attempting to read past the end.
func TestErrShort(t *testing.T) {
	var w Writer
	w.WriteBits(0b10101, 5) // write only 5 bits
	buf := w.Finish()

	r := NewReader(buf, 5)

	// First read 3 bits: OK
	if v, err := r.ReadBits(3); err != nil || v != 0b101 {
		t.Fatalf("first read: v=%b err=%v", v, err)
	}
	// Second read 3 bits: only 2 bits left → should fail with ErrShort
	if _, err := r.ReadBits(3); !errors.Is(err, ErrShort) {
		t.Fatalf("expected ErrShort, got %v", err)
	}
}

// TestMasking verifies that WriteBits masks off any high bits beyond 'width'.
func TestMasking(t *testing.T) {
	var w Writer
	w.WriteBits(0xFFFFFFFF, 5) // only lowest 5 bits should be stored
	buf := w.Finish()

	r := NewReader(buf, 5)
	got, err := r.ReadBits(5)
	if err != nil {
		t.Fatalf("ReadBits failed: %v", err)
	}
	if want := uint32(0x1F); got != want {
		t.Fatalf("masking mismatch: got=0x%X want=0x%X", got, want)
	}
}

// TestRandomizedRoundTrip performs a deterministic randomized test over many
// (value,width) pairs to exercise different boundary conditions and byte layouts.
// This acts like a lightweight property test without external dependencies.
func TestRandomizedRoundTrip(t *testing.T) {
	seed := int64(42)
	rng := rand.New(rand.NewSource(seed))

	for iter := range 50 { // 50 independent runs
		// Generate a random sequence of up to ~100 writes with widths in [1,16]
		type pair struct {
			w int
			v uint32
		}
		var seq []pair
		totalBits := 0
		for range 100 {
			w := 1 + rng.Intn(16)
			v := uint32(rng.Uint64()) & mask(w)
			seq = append(seq, pair{w: w, v: v})
			totalBits += w
			// Keep test runtime small: stop early with some probability
			if rng.Float64() < 0.05 {
				break
			}
		}
		// Write
		var w Writer
		for _, p := range seq {
			w.WriteBits(p.v, p.w)
		}
		buf := w.Finish()

		// Basic size sanity
		if got, want := len(buf), bitsToBytes(totalBits); got != want {
			t.Fatalf("iter %d: buf size mismatch: got %d want %d (bits=%d)", iter, got, want, totalBits)
		}

		// Read back
		r := NewReader(buf, totalBits)
		for i, p := range seq {
			got, err := r.ReadBits(p.w)
			if err != nil {
				t.Fatalf("iter %d step %d: ReadBits error: %v", iter, i, err)
			}
			if want := p.v & mask(p.w); got != want {
				t.Fatalf("iter %d step %d: mismatch got=0x%X want=0x%X (w=%d)", iter, i, got, want, p.w)
			}
		}
	}

	_ = time.Now() // keep linter calm if time imported in future tweaks
}
