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

func asn1IsASCII(b byte) bool {
	return b <= 0x7f
}

func asn1IsASCIISpace(b byte) bool {
	switch b {
	case ' ', '\t', '\n', '\v', '\f', '\r':
		return true
	default:
		return false
	}
}

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

func marshalASN1Value(tag int, isCompound bool, contents []byte) []byte {
	identifier := byte(tag)
	if isCompound {
		identifier |= 0x20
	}

	out := []byte{identifier}
	out = appendASN1Length(out, len(contents))
	return append(out, contents...)
}

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

	// Intentionally we do not re-encode the outer sequence tag and length,
	// as openssl does not do this for hashing of the subject name
	return canonical, nil
}
