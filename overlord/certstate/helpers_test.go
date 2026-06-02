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

package certstate_test

import (
	"bytes"
	"encoding/asn1"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/overlord/certstate"
)

type helpersSuite struct{}

var _ = Suite(&helpersSuite{})

func (s *helpersSuite) TestAsn1IsCanonicalizedStringType(c *C) {
	for _, tag := range []int{
		asn1.TagUTF8String,
		asn1.TagBMPString,
		certstate.Asn1TagUniversalString,
		asn1.TagPrintableString,
		asn1.TagT61String,
		asn1.TagIA5String,
		certstate.Asn1TagVisibleString,
	} {
		c.Check(certstate.Asn1IsCanonicalizedStringType(tag), Equals, true,
			Commentf("tag %d should be a canonicalized string type", tag))
	}

	for _, tag := range []int{
		0,
		asn1.TagBoolean,
		asn1.TagInteger,
		asn1.TagBitString,
		asn1.TagOID,
		asn1.TagNumericString,
		asn1.TagSequence,
		asn1.TagSet,
		asn1.TagGeneralString,
	} {
		c.Check(certstate.Asn1IsCanonicalizedStringType(tag), Equals, false,
			Commentf("tag %d should not be a canonicalized string type", tag))
	}
}

func (s *helpersSuite) TestAsn1IsASCII(c *C) {
	c.Check(certstate.Asn1IsASCII(0x00), Equals, true)
	c.Check(certstate.Asn1IsASCII(0x7F), Equals, true)
	c.Check(certstate.Asn1IsASCII('A'), Equals, true)
	c.Check(certstate.Asn1IsASCII('z'), Equals, true)
	c.Check(certstate.Asn1IsASCII('0'), Equals, true)
	c.Check(certstate.Asn1IsASCII(0x80), Equals, false)
	c.Check(certstate.Asn1IsASCII(0xFF), Equals, false)
}

func (s *helpersSuite) TestAsn1IsASCIISpace(c *C) {
	for _, b := range []byte{' ', '\t', '\n', '\v', '\f', '\r'} {
		c.Check(certstate.Asn1IsASCIISpace(b), Equals, true,
			Commentf("byte 0x%02X should be ASCII space", b))
	}

	for _, b := range []byte{'a', 'Z', '0', '!', 0x00, 0x1F, 0x7E, 0x80} {
		c.Check(certstate.Asn1IsASCIISpace(b), Equals, false,
			Commentf("byte 0x%02X should not be ASCII space", b))
	}
}

func (s *helpersSuite) TestAppendASN1Length(c *C) {
	// Short form: lengths 0–127 encoded as a single byte.
	c.Check(certstate.AppendASN1Length(nil, 0), DeepEquals, []byte{0x00})
	c.Check(certstate.AppendASN1Length(nil, 1), DeepEquals, []byte{0x01})
	c.Check(certstate.AppendASN1Length(nil, 127), DeepEquals, []byte{0x7F})

	// Long form: 128+.
	c.Check(certstate.AppendASN1Length(nil, 128), DeepEquals, []byte{0x81, 0x80})
	c.Check(certstate.AppendASN1Length(nil, 255), DeepEquals, []byte{0x81, 0xFF})
	c.Check(certstate.AppendASN1Length(nil, 256), DeepEquals, []byte{0x82, 0x01, 0x00})
	c.Check(certstate.AppendASN1Length(nil, 0xFFFF), DeepEquals, []byte{0x82, 0xFF, 0xFF})
	c.Check(certstate.AppendASN1Length(nil, 0x10000), DeepEquals, []byte{0x83, 0x01, 0x00, 0x00})

	// Result is appended to any existing dst content.
	c.Check(certstate.AppendASN1Length([]byte{0xAA}, 5), DeepEquals, []byte{0xAA, 0x05})
}

func (s *helpersSuite) TestMarshalASN1Value(c *C) {
	// Primitive UTF8String.
	result := certstate.MarshalASN1Value(asn1.TagUTF8String, false, []byte("hi"))
	c.Check(result, DeepEquals, []byte{0x0C, 0x02, 'h', 'i'})

	// Compound SEQUENCE: tag 0x10 | compound bit 0x20 = 0x30.
	result = certstate.MarshalASN1Value(asn1.TagSequence, true, []byte{0x01, 0x02})
	c.Check(result, DeepEquals, []byte{0x30, 0x02, 0x01, 0x02})

	// Compound SET: tag 0x11 | compound bit 0x20 = 0x31.
	result = certstate.MarshalASN1Value(asn1.TagSet, true, []byte{0x03, 0x04})
	c.Check(result, DeepEquals, []byte{0x31, 0x02, 0x03, 0x04})

	// Long-form length encoding for contents >= 128 bytes.
	contents := make([]byte, 200)
	result = certstate.MarshalASN1Value(asn1.TagOctetString, false, contents)
	// tag 0x04, length 200 = 0x81 0xC8
	c.Check(result, DeepEquals, append([]byte{0x04, 0x81, 0xC8}, contents...))

	// Empty contents.
	result = certstate.MarshalASN1Value(asn1.TagNull, false, nil)
	c.Check(result, DeepEquals, []byte{0x05, 0x00})
}

func (s *helpersSuite) TestAsn1StringToUTF8Bytes(c *C) {
	// UTF8String: valid.
	rv := asn1.RawValue{Tag: asn1.TagUTF8String, Bytes: []byte("hello")}
	result, err := certstate.Asn1StringToUTF8Bytes(rv)
	c.Assert(err, IsNil)
	c.Check(string(result), Equals, "hello")

	// UTF8String: invalid UTF-8 bytes produce an error.
	rv = asn1.RawValue{Tag: asn1.TagUTF8String, Bytes: []byte{0xFF, 0xFE}}
	_, err = certstate.Asn1StringToUTF8Bytes(rv)
	c.Check(err, ErrorMatches, "invalid UTF8String")

	// UTF8String: empty input returns empty slice.
	rv = asn1.RawValue{Tag: asn1.TagUTF8String, Bytes: []byte{}}
	result, err = certstate.Asn1StringToUTF8Bytes(rv)
	c.Assert(err, IsNil)
	c.Check(len(result), Equals, 0)

	// PrintableString: ASCII bytes are mapped rune-by-rune.
	rv = asn1.RawValue{Tag: asn1.TagPrintableString, Bytes: []byte("Hello")}
	result, err = certstate.Asn1StringToUTF8Bytes(rv)
	c.Assert(err, IsNil)
	c.Check(string(result), Equals, "Hello")

	// T61String: same byte-to-rune mapping.
	rv = asn1.RawValue{Tag: asn1.TagT61String, Bytes: []byte{0x41, 0x42}} // "AB"
	result, err = certstate.Asn1StringToUTF8Bytes(rv)
	c.Assert(err, IsNil)
	c.Check(string(result), Equals, "AB")

	// IA5String.
	rv = asn1.RawValue{Tag: asn1.TagIA5String, Bytes: []byte("test")}
	result, err = certstate.Asn1StringToUTF8Bytes(rv)
	c.Assert(err, IsNil)
	c.Check(string(result), Equals, "test")

	// VisibleString.
	rv = asn1.RawValue{Tag: certstate.Asn1TagVisibleString, Bytes: []byte("visible")}
	result, err = certstate.Asn1StringToUTF8Bytes(rv)
	c.Assert(err, IsNil)
	c.Check(string(result), Equals, "visible")

	// BMPString: UTF-16BE encoding of "Hi" → [0x00,0x48, 0x00,0x69].
	rv = asn1.RawValue{Tag: asn1.TagBMPString, Bytes: []byte{0x00, 0x48, 0x00, 0x69}}
	result, err = certstate.Asn1StringToUTF8Bytes(rv)
	c.Assert(err, IsNil)
	c.Check(string(result), Equals, "Hi")

	// BMPString: surrogate pair for U+1F600 (😀).
	// High surrogate 0xD83D, low surrogate 0xDE00.
	rv = asn1.RawValue{Tag: asn1.TagBMPString, Bytes: []byte{0xD8, 0x3D, 0xDE, 0x00}}
	result, err = certstate.Asn1StringToUTF8Bytes(rv)
	c.Assert(err, IsNil)
	c.Check(string(result), Equals, "😀")

	// BMPString: odd-length byte slice is an error.
	rv = asn1.RawValue{Tag: asn1.TagBMPString, Bytes: []byte{0x00, 0x48, 0x00}}
	_, err = certstate.Asn1StringToUTF8Bytes(rv)
	c.Check(err, ErrorMatches, "invalid BMPString length")

	// UniversalString: UTF-32BE encoding of "H" → [0x00,0x00,0x00,0x48].
	rv = asn1.RawValue{Tag: certstate.Asn1TagUniversalString, Bytes: []byte{0x00, 0x00, 0x00, 0x48}}
	result, err = certstate.Asn1StringToUTF8Bytes(rv)
	c.Assert(err, IsNil)
	c.Check(string(result), Equals, "H")

	// UniversalString: UTF-32BE encoding of U+1F600 (😀).
	rv = asn1.RawValue{Tag: certstate.Asn1TagUniversalString, Bytes: []byte{0x00, 0x01, 0xF6, 0x00}}
	result, err = certstate.Asn1StringToUTF8Bytes(rv)
	c.Assert(err, IsNil)
	c.Check(string(result), Equals, "😀")

	// UniversalString: length not a multiple of 4 is an error.
	rv = asn1.RawValue{Tag: certstate.Asn1TagUniversalString, Bytes: []byte{0x00, 0x00, 0x00}}
	_, err = certstate.Asn1StringToUTF8Bytes(rv)
	c.Check(err, ErrorMatches, "invalid UniversalString length")

	// UniversalString: surrogate code point (U+D800) is not a valid Unicode scalar.
	rv = asn1.RawValue{Tag: certstate.Asn1TagUniversalString, Bytes: []byte{0x00, 0x00, 0xD8, 0x00}}
	_, err = certstate.Asn1StringToUTF8Bytes(rv)
	c.Check(err, ErrorMatches, "invalid UniversalString code point")

	// Unsupported tag.
	rv = asn1.RawValue{Tag: asn1.TagInteger, Bytes: []byte{0x01}}
	_, err = certstate.Asn1StringToUTF8Bytes(rv)
	c.Check(err, ErrorMatches, "unsupported ASN.1 string type 2")
}

func (s *helpersSuite) TestCanonicalizeASN1String(c *C) {
	decodeOutput := func(out []byte) (int, string) {
		var rv asn1.RawValue
		_, err := asn1.Unmarshal(out, &rv)
		c.Assert(err, IsNil)
		return rv.Tag, string(rv.Bytes)
	}

	// Leading/trailing whitespace is trimmed and ASCII is lowercased.
	rv := asn1.RawValue{Tag: asn1.TagPrintableString, Bytes: []byte("  Hello World  ")}
	result, err := certstate.CanonicalizeASN1String(rv)
	c.Assert(err, IsNil)
	tag, val := decodeOutput(result)
	c.Check(tag, Equals, asn1.TagUTF8String)
	c.Check(val, Equals, "hello world")

	// Multiple internal whitespace characters are collapsed to a single space.
	rv = asn1.RawValue{Tag: asn1.TagUTF8String, Bytes: []byte("Hello\t \nWorld")}
	result, err = certstate.CanonicalizeASN1String(rv)
	c.Assert(err, IsNil)
	_, val = decodeOutput(result)
	c.Check(val, Equals, "hello world")

	// A string consisting entirely of whitespace becomes empty.
	rv = asn1.RawValue{Tag: asn1.TagPrintableString, Bytes: []byte("   ")}
	result, err = certstate.CanonicalizeASN1String(rv)
	c.Assert(err, IsNil)
	_, val = decodeOutput(result)
	c.Check(val, Equals, "")

	// Non-ASCII UTF-8 bytes are preserved; only ASCII letters are lowercased.
	rv = asn1.RawValue{Tag: asn1.TagUTF8String, Bytes: []byte("Büro GmbH")}
	result, err = certstate.CanonicalizeASN1String(rv)
	c.Assert(err, IsNil)
	_, val = decodeOutput(result)
	c.Check(val, Equals, "büro gmbh")

	// BMPString input is decoded to UTF-8 before canonicalization.
	rv = asn1.RawValue{Tag: asn1.TagBMPString, Bytes: []byte{0x00, 0x48, 0x00, 0x49}} // "HI"
	result, err = certstate.CanonicalizeASN1String(rv)
	c.Assert(err, IsNil)
	tag, val = decodeOutput(result)
	c.Check(tag, Equals, asn1.TagUTF8String)
	c.Check(val, Equals, "hi")

	// Output is always re-encoded as UTF8String regardless of input type.
	rv = asn1.RawValue{Tag: asn1.TagIA5String, Bytes: []byte("ACME")}
	result, err = certstate.CanonicalizeASN1String(rv)
	c.Assert(err, IsNil)
	tag, _ = decodeOutput(result)
	c.Check(tag, Equals, asn1.TagUTF8String)

	// Invalid UTF8String propagates an error.
	rv = asn1.RawValue{Tag: asn1.TagUTF8String, Bytes: []byte{0xFF, 0xFE}}
	_, err = certstate.CanonicalizeASN1String(rv)
	c.Check(err, NotNil)
}

func (s *helpersSuite) TestCanonicalizeNameAttribute(c *C) {
	cnOID := asn1.ObjectIdentifier{2, 5, 4, 3}
	cnOIDBytes, err := asn1.Marshal(cnOID)
	c.Assert(err, IsNil)

	// String attribute value (PrintableString) is canonicalized to UTF8String.
	valBytes, err := asn1.Marshal(asn1.RawValue{Tag: asn1.TagPrintableString, Bytes: []byte("Hello World")})
	c.Assert(err, IsNil)
	atav := asn1.RawValue{
		Class:      0,
		Tag:        asn1.TagSequence,
		IsCompound: true,
		Bytes:      append(cnOIDBytes, valBytes...),
	}
	result, err := certstate.CanonicalizeNameAttribute(atav)
	c.Assert(err, IsNil)

	var seq asn1.RawValue
	_, err = asn1.Unmarshal(result, &seq)
	c.Assert(err, IsNil)
	c.Check(seq.Tag, Equals, asn1.TagSequence)

	seqRem := seq.Bytes
	var oidVal asn1.RawValue
	seqRem, err = asn1.Unmarshal(seqRem, &oidVal)
	c.Assert(err, IsNil)
	var strVal asn1.RawValue
	_, err = asn1.Unmarshal(seqRem, &strVal)
	c.Assert(err, IsNil)
	c.Check(strVal.Tag, Equals, asn1.TagUTF8String)
	c.Check(string(strVal.Bytes), Equals, "hello world")

	// Non-string attribute value (integer) is preserved byte-for-byte.
	intBytes, err := asn1.Marshal(42)
	c.Assert(err, IsNil)
	atav = asn1.RawValue{
		Class:      0,
		Tag:        asn1.TagSequence,
		IsCompound: true,
		Bytes:      append(cnOIDBytes, intBytes...),
	}
	result, err = certstate.CanonicalizeNameAttribute(atav)
	c.Assert(err, IsNil)

	_, err = asn1.Unmarshal(result, &seq)
	c.Assert(err, IsNil)
	seqRem = seq.Bytes
	seqRem, err = asn1.Unmarshal(seqRem, &oidVal)
	c.Assert(err, IsNil)
	var intVal asn1.RawValue
	_, err = asn1.Unmarshal(seqRem, &intVal)
	c.Assert(err, IsNil)
	c.Check(intVal.Tag, Equals, asn1.TagInteger)
	c.Check(intVal.FullBytes, DeepEquals, intBytes)

	// Malformed: wrong class.
	atav = asn1.RawValue{Class: 2, Tag: asn1.TagSequence, IsCompound: true}
	_, err = certstate.CanonicalizeNameAttribute(atav)
	c.Check(err, ErrorMatches, "malformed certificate subject")

	// Malformed: wrong tag (SET instead of SEQUENCE).
	atav = asn1.RawValue{Class: 0, Tag: asn1.TagSet, IsCompound: true}
	_, err = certstate.CanonicalizeNameAttribute(atav)
	c.Check(err, ErrorMatches, "malformed certificate subject")

	// Malformed: not compound.
	atav = asn1.RawValue{Class: 0, Tag: asn1.TagSequence, IsCompound: false}
	_, err = certstate.CanonicalizeNameAttribute(atav)
	c.Check(err, ErrorMatches, "malformed certificate subject")

	// Malformed: trailing bytes after the value field.
	atav = asn1.RawValue{
		Class:      0,
		Tag:        asn1.TagSequence,
		IsCompound: true,
		Bytes:      append(append(cnOIDBytes, intBytes...), 0xFF),
	}
	_, err = certstate.CanonicalizeNameAttribute(atav)
	c.Check(err, ErrorMatches, "malformed certificate subject")

	// Malformed: Bytes is empty — cannot unmarshal even the attribute type.
	atav = asn1.RawValue{Class: 0, Tag: asn1.TagSequence, IsCompound: true, Bytes: nil}
	_, err = certstate.CanonicalizeNameAttribute(atav)
	c.Check(err, NotNil)

	// Malformed: only the OID, no attribute value.
	atav = asn1.RawValue{Class: 0, Tag: asn1.TagSequence, IsCompound: true, Bytes: cnOIDBytes}
	_, err = certstate.CanonicalizeNameAttribute(atav)
	c.Check(err, NotNil)
}

func (s *helpersSuite) TestCanonicalSubjectNameDER(c *C) {
	cnOID := asn1.ObjectIdentifier{2, 5, 4, 3}
	cnOIDBytes, err := asn1.Marshal(cnOID)
	c.Assert(err, IsNil)

	// Helper: build a minimal DER subject from a single ATAV.
	makeSubject := func(atavContents []byte) []byte {
		atav := certstate.MarshalASN1Value(asn1.TagSequence, true, atavContents)
		rdn := certstate.MarshalASN1Value(asn1.TagSet, true, atav)
		return certstate.MarshalASN1Value(asn1.TagSequence, true, rdn)
	}

	// Helper: parse the first ATAV value from the canonical output.
	parseFirstValue := func(canonical []byte) (tag int, value string) {
		var rdn asn1.RawValue
		_, err := asn1.Unmarshal(canonical, &rdn)
		c.Assert(err, IsNil)
		var atavSeq asn1.RawValue
		_, err = asn1.Unmarshal(rdn.Bytes, &atavSeq)
		c.Assert(err, IsNil)
		rem := atavSeq.Bytes
		var oidVal asn1.RawValue
		rem, err = asn1.Unmarshal(rem, &oidVal)
		c.Assert(err, IsNil)
		var strVal asn1.RawValue
		_, err = asn1.Unmarshal(rem, &strVal)
		c.Assert(err, IsNil)
		return strVal.Tag, string(strVal.Bytes)
	}

	// String attributes are trimmed, whitespace-collapsed, and lowercased.
	cnValBytes, err := asn1.Marshal(asn1.RawValue{Tag: asn1.TagPrintableString, Bytes: []byte("  HELLO   WORLD  ")})
	c.Assert(err, IsNil)
	subject := makeSubject(append(cnOIDBytes, cnValBytes...))
	canonical, err := certstate.CanonicalSubjectNameDER(subject)
	c.Assert(err, IsNil)
	tag, val := parseFirstValue(canonical)
	c.Check(tag, Equals, asn1.TagUTF8String)
	c.Check(val, Equals, "hello world")

	// Multiple RDNs are all preserved in the output.
	oOID := asn1.ObjectIdentifier{2, 5, 4, 10}
	oOIDBytes, err := asn1.Marshal(oOID)
	c.Assert(err, IsNil)
	oValBytes, err := asn1.Marshal(asn1.RawValue{Tag: asn1.TagUTF8String, Bytes: []byte("Example")})
	c.Assert(err, IsNil)
	cnAtav := certstate.MarshalASN1Value(asn1.TagSequence, true, append(cnOIDBytes, cnValBytes...))
	oAtav := certstate.MarshalASN1Value(asn1.TagSequence, true, append(oOIDBytes, oValBytes...))
	rdn1 := certstate.MarshalASN1Value(asn1.TagSet, true, cnAtav)
	rdn2 := certstate.MarshalASN1Value(asn1.TagSet, true, oAtav)
	multiRDNSubject := certstate.MarshalASN1Value(asn1.TagSequence, true, append(rdn1, rdn2...))
	canonical, err = certstate.CanonicalSubjectNameDER(multiRDNSubject)
	c.Assert(err, IsNil)
	remaining := canonical
	var rdnList []asn1.RawValue
	for len(remaining) > 0 {
		var rdn asn1.RawValue
		remaining, err = asn1.Unmarshal(remaining, &rdn)
		c.Assert(err, IsNil)
		rdnList = append(rdnList, rdn)
	}
	c.Check(len(rdnList), Equals, 2)

	// Multi-valued RDN: ATAVs are sorted by their canonical DER encoding.
	// OID 2.5.4.10 (O) < OID 2.5.4.11 (OU), so O must come first after sorting.
	// Use same-length attribute values so the OID byte is the deciding factor.
	ouOID := asn1.ObjectIdentifier{2, 5, 4, 11}
	ouOIDBytes, err := asn1.Marshal(ouOID)
	c.Assert(err, IsNil)
	oShortValBytes, err := asn1.Marshal(asn1.RawValue{Tag: asn1.TagUTF8String, Bytes: []byte("A")})
	c.Assert(err, IsNil)
	ouShortValBytes, err := asn1.Marshal(asn1.RawValue{Tag: asn1.TagUTF8String, Bytes: []byte("B")})
	c.Assert(err, IsNil)
	oAtav2 := certstate.MarshalASN1Value(asn1.TagSequence, true, append(oOIDBytes, oShortValBytes...))
	ouAtav := certstate.MarshalASN1Value(asn1.TagSequence, true, append(ouOIDBytes, ouShortValBytes...))
	// Place OU before O in the SET (out of canonical order).
	multiValuedRDN := certstate.MarshalASN1Value(asn1.TagSet, true, append(ouAtav, oAtav2...))
	subject3 := certstate.MarshalASN1Value(asn1.TagSequence, true, multiValuedRDN)
	canonical, err = certstate.CanonicalSubjectNameDER(subject3)
	c.Assert(err, IsNil)
	var rdnSet asn1.RawValue
	_, err = asn1.Unmarshal(canonical, &rdnSet)
	c.Assert(err, IsNil)
	setRem := rdnSet.Bytes
	var first, second asn1.RawValue
	setRem, err = asn1.Unmarshal(setRem, &first)
	c.Assert(err, IsNil)
	_, err = asn1.Unmarshal(setRem, &second)
	c.Assert(err, IsNil)
	// After sorting, O (2.5.4.10) must appear before OU (2.5.4.11).
	c.Check(bytes.Compare(first.FullBytes, second.FullBytes) < 0, Equals, true)
	// Confirm the first OID is O (2.5.4.10).
	firstRem := first.Bytes
	var firstOIDRaw asn1.RawValue
	_, err = asn1.Unmarshal(firstRem, &firstOIDRaw)
	c.Assert(err, IsNil)
	var firstOID asn1.ObjectIdentifier
	_, err = asn1.Unmarshal(firstOIDRaw.FullBytes, &firstOID)
	c.Assert(err, IsNil)
	c.Check(firstOID, DeepEquals, oOID)

	// Error: malformed DER input.
	_, err = certstate.CanonicalSubjectNameDER([]byte{0xFF, 0xFF})
	c.Check(err, NotNil)

	// Error: trailing bytes after the outer SEQUENCE.
	_, err = certstate.CanonicalSubjectNameDER(append(subject, 0x00))
	c.Check(err, ErrorMatches, "malformed certificate subject")

	// Error: outer element is not a SEQUENCE.
	notSeq := certstate.MarshalASN1Value(asn1.TagSet, true, rdn1)
	_, err = certstate.CanonicalSubjectNameDER(notSeq)
	c.Check(err, ErrorMatches, "malformed certificate subject")

	// Error: RDN element inside the SEQUENCE is not a SET.
	badRDN := certstate.MarshalASN1Value(asn1.TagSequence, true, cnAtav) // SEQUENCE instead of SET
	_, err = certstate.CanonicalSubjectNameDER(certstate.MarshalASN1Value(asn1.TagSequence, true, badRDN))
	c.Check(err, ErrorMatches, "malformed certificate subject")
}
