package apparmor

import (
	"bytes"
	"fmt"
)

// StringPacker assists in packing apparmor data structures with
// variable length string elements. It implements the static offset
// and ensures that non-empty strings are properly terminated.
type StringPacker struct {
	BaseOffset uint16
	Buffer     bytes.Buffer
}

// PackString computes the layout of a string encoded in an apparmor message.
// The return value is the offset of the beggining of the string relative to
// the start of the fixed portion of the structure, captured by BaseOffset.
//
// Empty strings use a special encoding that requires no space and used a fixed
// dummy offset of zero. The actual string is always nil-terminated, for
// compatibility with C.
func (sp *StringPacker) PackString(s string) uint32 {
	if s == "" {
		return 0
	}
	offset := uint32(sp.Buffer.Len())
	sp.Buffer.WriteString(s)
	sp.Buffer.WriteRune(0)
	return offset + uint32(sp.BaseOffset)
}

// StringUnpacker assists in unpacking apparmor data structures with
// variable length string elements.
type StringUnpacker struct {
	Bytes []byte
}

// UnpackString unpacks NUL-terminated string at a given offset into the buffer.
func (su *StringUnpacker) UnpackString(offset uint32) (string, error) {
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
