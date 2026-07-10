package qrcode

// pickVersion returns the smallest version whose byte-mode payload capacity
// holds dataLen bytes at the given level, or ErrTooLarge if it exceeds v40.
func pickVersion(dataLen int, level Level) (int, error) {
	for v := 1; v <= 40; v++ {
		// Available data bits minus mode indicator (4) and char-count width.
		capBits := dataCapacityBytes(v, level)*8 - 4 - charCountBits(v)
		if dataLen*8 <= capBits {
			return v, nil
		}
	}
	return 0, ErrTooLarge
}

// encodeData builds the full data-codeword block for a byte-mode payload: mode
// indicator, char count, data bytes, terminator, bit padding, then alternating
// pad codewords (0xEC, 0x11) to fill the version/level data capacity. The
// returned slice length equals the total data codewords for version/level.
func encodeData(data []byte, version int, level Level) []byte {
	total := dataCodewords(version, level)
	bs := newBitBuffer(total)
	bs.appendBits(0b0100, 4) // byte mode indicator
	bs.appendBits(uint(len(data)), charCountBits(version))
	for _, b := range data {
		bs.appendBits(uint(b), 8)
	}
	// Terminator: up to 4 zero bits, not exceeding capacity.
	remaining := total*8 - bs.length()
	term := 4
	if remaining < term {
		term = remaining
	}
	bs.appendBits(0, term)
	// Pad to a byte boundary.
	if pad := (8 - bs.length()%8) % 8; pad > 0 {
		bs.appendBits(0, pad)
	}
	// Pad codewords 0xEC, 0x11 alternating until the data capacity is filled.
	out := bs.bytes()
	for i := len(out); i < total; i++ {
		if (i-len(out))%2 == 0 {
			out = append(out, 0xEC)
		} else {
			out = append(out, 0x11)
		}
	}
	return out
}

// bitBuffer accumulates a big-endian bit stream into a byte slice, one bit at a
// time (MSB first within each byte).
type bitBuffer struct {
	buf   []byte
	nbits int
}

// newBitBuffer returns a bitBuffer preallocated for capBytes bytes.
func newBitBuffer(capBytes int) *bitBuffer {
	return &bitBuffer{buf: make([]byte, 0, capBytes)}
}

// length returns the number of bits appended so far.
func (b *bitBuffer) length() int { return b.nbits }

// appendBits appends the low n bits of v, most-significant bit first.
func (b *bitBuffer) appendBits(v uint, n int) {
	for i := n - 1; i >= 0; i-- {
		bit := byte((v >> uint(i)) & 1)
		if b.nbits%8 == 0 {
			b.buf = append(b.buf, 0)
		}
		b.buf[b.nbits/8] |= bit << uint(7-b.nbits%8)
		b.nbits++
	}
}

// bytes returns the buffered bytes. The final byte is right-padded with zeros
// when the bit count is not a multiple of eight (encodeData pads to a byte
// boundary before calling this).
func (b *bitBuffer) bytes() []byte { return b.buf }

// finalCodewords encodes data, splits it into the version/level's EC blocks,
// computes each block's EC codewords, and interleaves data then EC codewords
// per ISO/IEC 18004 §8.6.
func finalCodewords(data []byte, version int, level Level) []byte {
	s := versionSpec(version, level)
	all := encodeData(data, version, level)

	// Split into blocks.
	type block struct{ data, ec []byte }
	blocks := make([]block, 0, s.group1Blocks+s.group2Blocks)
	pos := 0
	addBlocks := func(count, words int) {
		for range count {
			d := all[pos : pos+words]
			pos += words
			blocks = append(blocks, block{data: d, ec: ecCodewords(d, s.ecPerBlock)})
		}
	}
	addBlocks(s.group1Blocks, s.group1Words)
	addBlocks(s.group2Blocks, s.group2Words)

	out := make([]byte, 0, len(all)+len(blocks)*s.ecPerBlock)
	// Interleave data codewords column-by-column across blocks.
	maxData := s.group1Words
	if s.group2Words > maxData {
		maxData = s.group2Words
	}
	for i := range maxData {
		for _, b := range blocks {
			if i < len(b.data) {
				out = append(out, b.data[i])
			}
		}
	}
	// Interleave EC codewords column-by-column across blocks.
	for i := range s.ecPerBlock {
		for _, b := range blocks {
			out = append(out, b.ec[i])
		}
	}
	return out
}
