// -*- Mode: Go; indent-tabs-mode: t -*-
/*
 * Copyright (C) 2026 Canonical Ltd
 *
 * This program is free software: you can redistribute it and/or modify
 * it under the terms of the GNU General Public License version 3 as
 * published by the Free Software Foundation.
 *
 * This program is distributed in the hope that it will be useful,
 * but WITHOUT ANY WARRANTY; without even the implied warranty of
 * MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
 * GNU General Public License for more details.
 *
 * You should have received a copy of the GNU General Public License
 * along with this program.  If not, see <http://www.gnu.org/licenses/>.
 *
 */

package certstate

import (
	"bytes"
	"encoding/asn1"
	"encoding/binary"
	"fmt"
	"sort"
	"unicode/utf16"
	"unicode/utf8"
)

const (
	asn1TagVisibleString   = 26
	asn1TagUniversalString = 28
)

// asn1IsCanonicalizedStringType reports whether tag is one of the ASN.1
// string types that OpenSSL normalises before hashing an X.509 name.
func asn1IsCanonicalizedStringType(tag int) bool {
	switch tag {
	case asn1.TagUTF8String, asn1.TagBMPString, asn1TagUniversalString,
		asn1.TagPrintableString, asn1.TagT61String, asn1.TagIA5String,
		asn1TagVisibleString:
		return true
	default:
		return false
	}
}

// asn1IsASCII reports whether b is an ASCII byte.
func asn1IsASCII(b byte) bool {
	return b <= 0x7f
}

// asn1IsASCIISpace reports whether b is an ASCII whitespace byte that is
// trimmed or collapsed during OpenSSL-style string canonicalisation.
func asn1IsASCIISpace(b byte) bool {
	switch b {
	case ' ', '\t', '\n', '\v', '\f', '\r':
		return true
	default:
		return false
	}
}

// appendASN1Length appends the DER length encoding for length to dst.
func appendASN1Length(dst []byte, length int) []byte {
	if length < 0x80 {
		return append(dst, byte(length))
	}

	var encoded [8]byte
	count := 0
	for length > 0 {
		encoded[len(encoded)-1-count] = byte(length)
		length >>= 8
		count++
	}
	dst = append(dst, 0x80|byte(count))
	return append(dst, encoded[len(encoded)-count:]...)
}

// marshalASN1Value builds a DER TLV with the given universal tag, primitive or
// constructed bit, and already-encoded contents.
func marshalASN1Value(tag int, isCompound bool, contents []byte) []byte {
	identifier := byte(tag)
	if isCompound {
		identifier |= 0x20
	}

	out := []byte{identifier}
	out = appendASN1Length(out, len(contents))
	return append(out, contents...)
}

// asn1StringToUTF8Bytes decodes the supported ASN.1 string encodings used in
// distinguished names into UTF-8 bytes.
func asn1StringToUTF8Bytes(value asn1.RawValue) ([]byte, error) {
	switch value.Tag {
	case asn1.TagUTF8String:
		if !utf8.Valid(value.Bytes) {
			return nil, fmt.Errorf("invalid UTF8String")
		}
		return append([]byte(nil), value.Bytes...), nil
	case asn1.TagPrintableString, asn1.TagT61String, asn1.TagIA5String, asn1TagVisibleString:
		runes := make([]rune, 0, len(value.Bytes))
		for _, b := range value.Bytes {
			runes = append(runes, rune(b))
		}
		return []byte(string(runes)), nil
	case asn1.TagBMPString:
		if len(value.Bytes)%2 != 0 {
			return nil, fmt.Errorf("invalid BMPString length")
		}
		codeUnits := make([]uint16, 0, len(value.Bytes)/2)
		for i := 0; i < len(value.Bytes); i += 2 {
			codeUnits = append(codeUnits, binary.BigEndian.Uint16(value.Bytes[i:i+2]))
		}
		return []byte(string(utf16.Decode(codeUnits))), nil
	case asn1TagUniversalString:
		if len(value.Bytes)%4 != 0 {
			return nil, fmt.Errorf("invalid UniversalString length")
		}
		runes := make([]rune, 0, len(value.Bytes)/4)
		for i := 0; i < len(value.Bytes); i += 4 {
			codePoint := rune(binary.BigEndian.Uint32(value.Bytes[i : i+4]))
			if !utf8.ValidRune(codePoint) {
				return nil, fmt.Errorf("invalid UniversalString code point")
			}
			runes = append(runes, codePoint)
		}
		return []byte(string(runes)), nil
	default:
		return nil, fmt.Errorf("unsupported ASN.1 string type %d", value.Tag)
	}
}

// canonicalizeASN1String applies the OpenSSL string normalisation used for
// X.509 name hashing:
//
//   - decode the original string value to UTF-8;
//   - trim leading and trailing ASCII whitespace;
//   - collapse internal ASCII whitespace runs to a single space;
//   - lower-case ASCII letters; and
//   - re-encode the result as a UTF8String.
func canonicalizeASN1String(value asn1.RawValue) ([]byte, error) {
	utf8Bytes, err := asn1StringToUTF8Bytes(value)
	if err != nil {
		return nil, err
	}

	start := 0
	for start < len(utf8Bytes) && asn1IsASCIISpace(utf8Bytes[start]) {
		start++
	}
	end := len(utf8Bytes)
	for end > start && asn1IsASCIISpace(utf8Bytes[end-1]) {
		end--
	}

	canonical := make([]byte, 0, end-start)
	for i := start; i < end; {
		b := utf8Bytes[i]
		switch {
		case !asn1IsASCII(b):
			canonical = append(canonical, b)
			i++
		case asn1IsASCIISpace(b):
			canonical = append(canonical, ' ')
			for i < end && asn1IsASCIISpace(utf8Bytes[i]) {
				i++
			}
		case 'A' <= b && b <= 'Z':
			canonical = append(canonical, b+('a'-'A'))
			i++
		default:
			canonical = append(canonical, b)
			i++
		}
	}

	return marshalASN1Value(asn1.TagUTF8String, false, canonical), nil
}

// canonicalizeNameAttribute canonicalises a single AttributeTypeAndValue
// sequence. String values are normalised with canonicalizeASN1String, while
// non-string values are preserved byte-for-byte.
func canonicalizeNameAttribute(rawATAV asn1.RawValue) ([]byte, error) {
	if rawATAV.Class != 0 || rawATAV.Tag != asn1.TagSequence || !rawATAV.IsCompound {
		return nil, fmt.Errorf("malformed certificate subject")
	}

	remaining := rawATAV.Bytes
	var attributeType asn1.RawValue
	var attributeValue asn1.RawValue
	var err error

	remaining, err = asn1.Unmarshal(remaining, &attributeType)
	if err != nil {
		return nil, err
	}
	remaining, err = asn1.Unmarshal(remaining, &attributeValue)
	if err != nil {
		return nil, err
	}
	if len(remaining) != 0 {
		return nil, fmt.Errorf("malformed certificate subject")
	}

	valueBytes := attributeValue.FullBytes
	if attributeValue.Class == 0 && !attributeValue.IsCompound && asn1IsCanonicalizedStringType(attributeValue.Tag) {
		valueBytes, err = canonicalizeASN1String(attributeValue)
		if err != nil {
			return nil, err
		}
	}

	contents := make([]byte, 0, len(attributeType.FullBytes)+len(valueBytes))
	contents = append(contents, attributeType.FullBytes...)
	contents = append(contents, valueBytes...)
	return marshalASN1Value(asn1.TagSequence, true, contents), nil
}

// canonicalSubjectNameDER returns the DER encoding of rawSubject after applying
// OpenSSL's X509_NAME_hash canonicalisation:
//
//   - every ASN.1 string attribute value is decoded to UTF-8, lower-cased,
//     stripped of leading/trailing whitespace, and internal whitespace runs
//     are collapsed to a single space;
//   - the resulting string is re-encoded as a UTF8String; and
//   - AttributeTypeAndValue entries within each RDN SET are sorted by their
//     canonical DER encoding.
//
// The inner SET values are re-encoded after canonicalisation. The outer
// SEQUENCE wrapper is not re-emitted; callers receive the canonicalised
// SEQUENCE contents.
func canonicalSubjectNameDER(rawSubject []byte) ([]byte, error) {
	var subject asn1.RawValue
	rest, err := asn1.Unmarshal(rawSubject, &subject)
	if err != nil {
		return nil, err
	}
	if len(rest) != 0 || subject.Class != 0 || subject.Tag != asn1.TagSequence || !subject.IsCompound {
		return nil, fmt.Errorf("malformed certificate subject")
	}

	remaining := subject.Bytes
	canonical := make([]byte, 0, len(rawSubject))
	for len(remaining) > 0 {
		var rdn asn1.RawValue
		remaining, err = asn1.Unmarshal(remaining, &rdn)
		if err != nil {
			return nil, err
		}
		if rdn.Class != 0 || rdn.Tag != asn1.TagSet || !rdn.IsCompound {
			return nil, fmt.Errorf("malformed certificate subject")
		}

		setRemaining := rdn.Bytes
		encodedATAVs := make([][]byte, 0, 1)
		for len(setRemaining) > 0 {
			var atav asn1.RawValue
			setRemaining, err = asn1.Unmarshal(setRemaining, &atav)
			if err != nil {
				return nil, err
			}

			encodedATAV, err := canonicalizeNameAttribute(atav)
			if err != nil {
				return nil, err
			}
			encodedATAVs = append(encodedATAVs, encodedATAV)
		}

		sort.Slice(encodedATAVs, func(i, j int) bool {
			return bytes.Compare(encodedATAVs[i], encodedATAVs[j]) < 0
		})

		setContents := make([]byte, 0, len(rdn.FullBytes))
		for _, encodedATAV := range encodedATAVs {
			setContents = append(setContents, encodedATAV...)
		}
		canonical = append(canonical, marshalASN1Value(asn1.TagSet, true, setContents)...)
	}

	// Intentionally return only the canonicalised SEQUENCE contents.
	return canonical, nil
}
