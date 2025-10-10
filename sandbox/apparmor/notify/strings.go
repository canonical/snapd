package notify

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"sort"
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

// packString writes the given string into the packer returns the offset of the
// beginning of the string relative to the start of the structure.
//
// Empty strings use a special encoding that requires no space and always return
// a fixed offset of zero to indicate that the string is empty. The actual
// string is always nil-terminated, for compatibility with C.
func (sp *stringPacker) packString(s string) uint32 {
	if s == "" {
		return 0
	}
	offset := uint32(sp.buffer.Len())
	sp.buffer.WriteString(s)
	sp.buffer.WriteRune(0)
	return offset + uint32(sp.baseOffset)
}

// packTagsets computes headers for the given tagsets, packs the tagsets and
// headers, and returns the offset of the beginning of the first header
// relative to the start of the structure.
//
// If there are no tagsets, nothing new is packed, and returns 0.
//
// Tagset headers are contiguous, and the start of the first header must be
// 8-byte-aligned. Each header contains information about a tagset, including
// its associated permission mask, the number of tags in the tagset, and the
// offset of the beginning of the first tag, again relative to the start of the
// structure.
//
// Any tagset which does not have any tags is ignored.
//
// The tagsets themselves may occur before or after the tagset headers, and
// need not be contiguous, either between tagsets or with the tagset headers.
// All the tags in any given tagset must be contiguous, however.
//
// For code simplicity, we encode all the tagsets first, then the headers.
// By convention, there is an additional \0 byte after the end of each tagset,
// but in the future, tagsets may overlap to save space, so this should not be
// relied upon, so we do not include it here.
//
// It should never be necessary to pack tagsets outside of test code, since
// snapd should never need to send a message containing tagsets to the kernel.
func (sp *stringPacker) packTagsets(ts TagsetMap) uint32 {
	if len(ts) == 0 {
		return 0
	}
	headers := make([]tagsetHeader, len(ts))

	// Make marshalled message deterministic by including tagsets sorted by
	// their associated permissions.
	perms := make([]AppArmorPermission, 0, len(ts))
	for perm := range ts {
		perms = append(perms, perm)
	}
	// TODO:GOVERSION: use slices.Sort() once we're on go 1.21+
	sort.Slice(perms, func(i, j int) bool {
		return perms[i].AsAppArmorOpMask() < perms[j].AsAppArmorOpMask()
	})

	for i, perm := range perms {
		tags := ts[perm]
		headers[i].PermissionMask = perm.AsAppArmorOpMask()
		headers[i].TagCount = uint32(len(tags))
		if len(tags) == 0 {
			// tagset has no tags, so set offset to 0. Still include the tagset
			// in the message, since it may be meaningful that there are no
			// tags for the given permission mask.
			headers[i].TagOffset = 0
			continue
		}
		// Pack the first tag, and set the tagset's tag offset to its start
		headers[i].TagOffset = sp.packString(tags[0])
		// Pack the rest of the tags in this tagset immediately following
		for _, tag := range tags[1:] {
			sp.packString(tag)
		}
	}

	// Now add padding to align the tagset headers
	totalLength := sp.buffer.Len() + int(sp.baseOffset)
	alignmentPadding := make([]byte, totalLength%8)
	sp.buffer.Write(alignmentPadding)

	headerOffset := uint32(sp.buffer.Len())

	// Now write the headers themselves
	binary.Write(&sp.buffer, nativeByteOrder, headers)

	return headerOffset + uint32(sp.baseOffset)
}

// totalLen returns the total length of the data which is being packed,
// equal to the base offset plus the length of the data buffer.
func (sp *stringPacker) totalLen() uint16 {
	return sp.baseOffset + uint16(sp.buffer.Len())
}

// bytes returns the underlying byte array into which data base been packed.
func (sp *stringPacker) bytes() []byte {
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

// unpackString unpacks NUL-terminated string at a given offset into the buffer.
func (su *stringUnpacker) unpackString(offset uint32) (string, error) {
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

// unpackStrings unpacks N contiguous NUL-terminated strings at a given offset
// into the buffer.
func (su *stringUnpacker) unpackStrings(offset uint32, n uint32) ([]string, error) {
	if offset == 0 || n == 0 {
		return nil, nil
	}
	if offset >= uint32(len(su.Bytes)) {
		return nil, fmt.Errorf("address %d points outside of message body", offset)
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
