package notify

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"sort"

	"github.com/snapcore/snapd/arch"
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

// PackTagsets computes the layout of the tagsets encoded in an apparmor message.
// Tagset headers are contiguous, and the start of the first header must be
// 8-byte-aligned. The return value is the offset of the beginning of the first
// header relative to the start of the structure, captured by baseOffset.
//
// Each header contains information about a tagset, including its associated
// permissions, the number of tags in the tagset, and the offset of the
// beginning of the first tag, again relative to the start of the structure.
//
// The tagsets themselves may occur before or after the tagset headers, and
// need not be contiguous, either between tagsets or with the tagset headers.
// All the tags in any given tagset must be contiguous, however.
//
// For code simplicity, we encode all the tagsets first, then the headers.
// By convention, there is an additional \0 byte after the end of each tagset,
// but in the future, tagsets may overlap to save space, so this should not be
// relied upon, so we do not include it here.
func (sp *stringPacker) PackTagsets(ts map[uint32][]string) uint32 {
	if len(ts) == 0 {
		return 0
	}
	headers := make([]tagsetHeader, len(ts))
	i := 0

	// Make marshalled message deterministic by including tagsets sorted by
	// their associated permissions.
	perms := make([]uint32, 0, len(ts))
	for perm := range ts {
		perms = append(perms, perm)
	}
	// TODO: use slices.Sort() once we're on go 1.21+
	sort.Slice(perms, func(i, j int) bool {
		return perms[i] < perms[j]
	})

	for _, perm := range perms {
		tags := ts[perm]
		if len(tags) == 0 {
			continue
		}
		headers[i].PermissionMask = perm
		headers[i].TagCount = uint32(len(tags))
		headers[i].TagOffset = sp.PackString(tags[0])
		for _, tag := range tags[1:] {
			sp.PackString(tag)
		}
		i++
	}

	// Now add padding to align the tagset headers
	totalLength := sp.buffer.Len() + int(sp.baseOffset)
	alignmentPadding := make([]byte, totalLength%8)
	sp.buffer.Write(alignmentPadding)

	headerOffset := uint32(sp.buffer.Len())

	// Now write the headers themselves
	order := arch.Endian()
	binary.Write(&sp.buffer, order, headers)

	return headerOffset + uint32(sp.baseOffset)
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

// UnpackStrings unpacks N contiguous NUL-terminated strings at a given offset
// into the buffer.
func (su *stringUnpacker) UnpackStrings(offset uint32, n uint32) ([]string, error) {
	if offset == 0 {
		return nil, nil
	}
	if offset >= uint32(len(su.Bytes)) {
		return nil, fmt.Errorf("address %d points outside of message body", offset)
	}
	if n == 0 {
		return nil, nil
	}
	strs := make([]string, n)
	for i := uint32(0); i < n; i++ {
		tmp := su.Bytes[offset:]
		idx := bytes.IndexByte(tmp, 0)
		if idx < 0 {
			return nil, fmt.Errorf("unterminated string at address %d", offset)
		}
		strs[i] = string(tmp[:idx])
		offset += uint32(idx) + 1 // advance offset to start of next string
	}
	return strs, nil
}
