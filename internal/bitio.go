package bitio

import "errors"

// Writer is a bit-level writer that allows writing arbitrary numbers of bits into a byte buffer.
// The bits are accumulated in 'acc' until at least 8 bits are available, at which point a byte is flushed to 'Buf'.
type Writer struct {
	Buf   []byte // Output buffer where bytes are written as they are completed.
	acc   uint64 // Bit accumulator; holds bits that have not yet been flushed to the buffer.
	nbits int    // Number of bits currently stored in the accumulator.
}

// WriteBits writes the lowest 'width' bits of 'v' into the buffer.
// Bits are accumulated in 'acc' until at least 8 bits are available,
// at which point a byte is flushed to the buffer.
func (w *Writer) WriteBits(v uint32, width int) {
	// Mask the input value to only keep the lowest 'width' bits,
	// then shift it into the accumulator at the current bit position.
	w.acc |= uint64(v&((1<<width)-1)) << w.nbits
	// Increase the number of bits currently in the accumulator.
	w.nbits += width
	// While there are at least 8 bits in the accumulator,
	// flush the lowest byte to the buffer.
	for w.nbits >= 8 {
		w.Buf = append(w.Buf, byte(w.acc&0xff)) // Write out the lowest 8 bits.
		w.acc >>= 8                             // Remove the written bits from the accumulator.
		w.nbits -= 8                            // Decrease the bit count by 8.
	}
}

// Finish flushes any remaining bits in the accumulator to the buffer as a final byte.
// If there are leftover bits (less than 8), they are written as the lowest bits of the last byte.
// After flushing, the accumulator and bit count are reset to zero.
func (w *Writer) Finish() []byte {
	if w.nbits > 0 {
		// Write the remaining bits (less than 8) as a single byte.
		w.Buf = append(w.Buf, byte(w.acc&0xff))
		w.acc, w.nbits = 0, 0 // Reset accumulator and bit count.
	}
	return w.Buf
}

// Reader reads bits from a byte slice, accumulating bits in 'acc'.
// 'cur' tracks the current position in the source byte slice.
// 'nbits' is the number of bits currently in the accumulator.
type Reader struct {
	Src   []byte // Source byte slice to read from.
	acc   uint64 // Bit accumulator.
	nbits int    // Number of bits currently in the accumulator.
	cur   int    // Current byte index in Src.
	rem   int    // Remaining valid bits in the logical stream (excludes zero-padding).
}

// ErrShort is returned when there are not enough bytes left in the source to satisfy a read.
var ErrShort = errors.New("deckcodec/bitio: unexpected EOF")

// NewReader constructs a Reader with a known number of valid bits.
// If validBits < 0, all bits in src are considered valid (len(src)*8).
func NewReader(src []byte, validBits int) Reader {
	if validBits < 0 {
		validBits = len(src) * 8
	}
	return Reader{Src: src, rem: validBits}
}

// ReadBits reads 'width' bits from the source and returns them as a uint32.
// If there are not enough bits in the accumulator, it loads more bytes from Src.
// Returns ErrShort if the source runs out of bytes before enough bits are available.
func (r *Reader) ReadBits(width int) (uint32, error) {
	// First check: logically enough bits remain?
	if width < 0 {
		return 0, ErrShort
	}
	if r.rem < width {
		return 0, ErrShort
	}
	// Ensure there are at least 'width' bits in the accumulator.
	for r.nbits < width {
		if r.cur >= len(r.Src) {
			// Not enough bytes left in the source to read the requested number of bits.
			return 0, ErrShort
		}
		// Load the next byte into the accumulator at the current bit position.
		r.acc |= uint64(r.Src[r.cur]) << r.nbits
		r.cur++
		r.nbits += 8
	}
	// Mask to extract only the requested number of bits.
	mask := uint64((1 << width) - 1)
	out := uint32(r.acc & mask) // Extract the lowest 'width' bits.
	r.acc >>= width             // Remove the extracted bits from the accumulator.
	r.nbits -= width            // Decrease the bit count.
	r.rem -= width              // Consume valid bits.
	return out, nil
}
