package notify

import (
	"bytes"
	"encoding/binary"
	"fmt"
)

// stringPacker assists in packing apparmor data structures with
// variable length string elements. It implements the static offset
// and ensures that non-empty strings are properly terminated.
type stringPacker struct {
	baseOffset uint16
	buffer     bytes.Buffer
}

// newStringPacker returns a new string packer for the given struct.
// The base offset is set to be the size of the given struct.
func newStringPacker(rawStruct any) stringPacker {
	return stringPacker{
		baseOffset: uint16(binary.Size(rawStruct)),
	}
}

// PackString computes the layout of a string encoded in an apparmor message.
// The return value is the offset of the beginning of the string relative to
// the start of the fixed portion of the structure, captured by baseOffset.
//
// Empty strings use a special encoding that requires no space and always return
// a fixed offset of zero to indicate that the string is empty. The actual
// string is always nil-terminated, for compatibility with C.
func (sp *stringPacker) PackString(s string) uint32 {
	if s == "" {
		return 0
	}
	offset := uint32(sp.buffer.Len())
	sp.buffer.WriteString(s)
	sp.buffer.WriteRune(0)
	return offset + uint32(sp.baseOffset)
}

// TotalLen returns the total length of the data which is being packed,
// equal to the base offset plus the length of the data buffer.
func (sp *stringPacker) TotalLen() uint16 {
	return sp.baseOffset + uint16(sp.buffer.Len())
}

// Bytes returns the underlying byte array into which data base been packed.
func (sp *stringPacker) Bytes() []byte {
	return sp.buffer.Bytes()
}

// stringUnpacker assists in unpacking apparmor data structures with
// variable length string elements.
type stringUnpacker struct {
	Bytes []byte
}

// newStringUnpacker returns a new string unpacker for the given data.
func newStringUnpacker(data []byte) stringUnpacker {
	return stringUnpacker{
		Bytes: data,
	}
}

// UnpackString unpacks NUL-terminated string at a given offset into the buffer.
func (su *stringUnpacker) UnpackString(offset uint32) (string, error) {
	if offset == 0 {
		return "", nil
	}
	if offset >= uint32(len(su.Bytes)) {
		return "", fmt.Errorf("address %d points outside of message body", offset)
	}
	tmp := su.Bytes[offset:]
	idx := bytes.IndexByte(tmp, 0)
	if idx < 0 {
		return "", fmt.Errorf("unterminated string at address %d", offset)
	}
	return string(tmp[:idx]), nil
}
