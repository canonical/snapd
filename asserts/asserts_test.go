// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2015-2016 Canonical Ltd
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

package asserts_test

import (
	"bytes"
	"io"
	"strings"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/asserts"
)

type assertsSuite struct{}

var _ = Suite(&assertsSuite{})

func (as *assertsSuite) TestType(c *C) {
	c.Check(asserts.Type("test-only"), Equals, asserts.TestOnlyType)
}

func (as *assertsSuite) TestUnknown(c *C) {
	c.Check(asserts.Type(""), IsNil)
	c.Check(asserts.Type("unknown"), IsNil)
}

func (as *assertsSuite) TestTypeMaxSupportedFormat(c *C) {
	c.Check(asserts.Type("test-only").MaxSupportedFormat(), Equals, 1)
}

func (as *assertsSuite) TestTypeNames(c *C) {
	c.Check(asserts.TypeNames(), DeepEquals, []string{
		"account",
		"account-key",
		"account-key-request",
		"base-declaration",
		"device-session-request",
		"model",
		"repair",
		"serial",
		"serial-request",
		"snap-build",
		"snap-declaration",
		"snap-developer",
		"snap-revision",
		"store",
		"system-user",
		"test-only",
		"test-only-2",
		"test-only-no-authority",
		"test-only-no-authority-pk",
		"validation",
	})
}

func (as *assertsSuite) TestSuggestFormat(c *C) {
	fmtnum, err := asserts.SuggestFormat(asserts.Type("test-only-2"), nil, nil)
	c.Assert(err, IsNil)
	c.Check(fmtnum, Equals, 0)
}

func (as *assertsSuite) TestPrimaryKeyHelpers(c *C) {
	headers, err := asserts.HeadersFromPrimaryKey(asserts.TestOnlyType, []string{"one"})
	c.Assert(err, IsNil)
	c.Check(headers, DeepEquals, map[string]string{
		"primary-key": "one",
	})

	headers, err = asserts.HeadersFromPrimaryKey(asserts.TestOnly2Type, []string{"bar", "baz"})
	c.Assert(err, IsNil)
	c.Check(headers, DeepEquals, map[string]string{
		"pk1": "bar",
		"pk2": "baz",
	})

	_, err = asserts.HeadersFromPrimaryKey(asserts.TestOnly2Type, []string{"bar"})
	c.Check(err, ErrorMatches, `primary key has wrong length for "test-only-2" assertion`)

	_, err = asserts.HeadersFromPrimaryKey(asserts.TestOnly2Type, []string{"", "baz"})
	c.Check(err, ErrorMatches, `primary key "pk1" header cannot be empty`)

	pk, err := asserts.PrimaryKeyFromHeaders(asserts.TestOnly2Type, headers)
	c.Assert(err, IsNil)
	c.Check(pk, DeepEquals, []string{"bar", "baz"})

	headers["other"] = "foo"
	pk1, err := asserts.PrimaryKeyFromHeaders(asserts.TestOnly2Type, headers)
	c.Assert(err, IsNil)
	c.Check(pk1, DeepEquals, pk)

	delete(headers, "pk2")
	_, err = asserts.PrimaryKeyFromHeaders(asserts.TestOnly2Type, headers)
	c.Check(err, ErrorMatches, `must provide primary key: pk2`)
}

func (as *assertsSuite) TestRef(c *C) {
	ref := &asserts.Ref{
		Type:       asserts.TestOnly2Type,
		PrimaryKey: []string{"abc", "xyz"},
	}
	c.Check(ref.Unique(), Equals, "test-only-2/abc/xyz")
}

func (as *assertsSuite) TestRefString(c *C) {
	ref := &asserts.Ref{
		Type:       asserts.AccountType,
		PrimaryKey: []string{"canonical"},
	}

	c.Check(ref.String(), Equals, "account (canonical)")

	ref = &asserts.Ref{
		Type:       asserts.SnapDeclarationType,
		PrimaryKey: []string{"18", "SNAPID"},
	}

	c.Check(ref.String(), Equals, "snap-declaration (SNAPID; series:18)")

	ref = &asserts.Ref{
		Type:       asserts.ModelType,
		PrimaryKey: []string{"18", "BRAND", "baz-3000"},
	}

	c.Check(ref.String(), Equals, "model (baz-3000; series:18 brand-id:BRAND)")

	// broken primary key
	ref = &asserts.Ref{
		Type:       asserts.ModelType,
		PrimaryKey: []string{"18"},
	}
	c.Check(ref.String(), Equals, "model (???)")

	ref = &asserts.Ref{
		Type: asserts.TestOnlyNoAuthorityType,
	}
	c.Check(ref.String(), Equals, "test-only-no-authority (-)")
}

func (as *assertsSuite) TestRefResolveError(c *C) {
	ref := &asserts.Ref{
		Type:       asserts.TestOnly2Type,
		PrimaryKey: []string{"abc"},
	}
	_, err := ref.Resolve(nil)
	c.Check(err, ErrorMatches, `"test-only-2" assertion reference primary key has the wrong length \(expected \[pk1 pk2\]\): \[abc\]`)
}

const exKeyID = "Jv8_JiHiIzJVcO9M55pPdqSDWUvuhfDIBJUS-3VW7F_idjix7Ffn5qMxB21ZQuij"

const exampleEmptyBodyAllDefaults = "type: test-only\n" +
	"authority-id: auth-id1\n" +
	"primary-key: abc\n" +
	"sign-key-sha3-384: Jv8_JiHiIzJVcO9M55pPdqSDWUvuhfDIBJUS-3VW7F_idjix7Ffn5qMxB21ZQuij" +
	"\n\n" +
	"AXNpZw=="

func (as *assertsSuite) TestDecodeEmptyBodyAllDefaults(c *C) {
	a, err := asserts.Decode([]byte(exampleEmptyBodyAllDefaults))
	c.Assert(err, IsNil)
	c.Check(a.Type(), Equals, asserts.TestOnlyType)
	_, ok := a.(*asserts.TestOnly)
	c.Check(ok, Equals, true)
	c.Check(a.Revision(), Equals, 0)
	c.Check(a.Format(), Equals, 0)
	c.Check(a.Body(), IsNil)
	c.Check(a.Header("header1"), IsNil)
	c.Check(a.HeaderString("header1"), Equals, "")
	c.Check(a.AuthorityID(), Equals, "auth-id1")
	c.Check(a.SignKeyID(), Equals, exKeyID)
}

const exampleEmptyBody2NlNl = "type: test-only\n" +
	"authority-id: auth-id1\n" +
	"primary-key: xyz\n" +
	"revision: 0\n" +
	"body-length: 0\n" +
	"sign-key-sha3-384: Jv8_JiHiIzJVcO9M55pPdqSDWUvuhfDIBJUS-3VW7F_idjix7Ffn5qMxB21ZQuij" +
	"\n\n" +
	"\n\n" +
	"AXNpZw==\n"

func (as *assertsSuite) TestDecodeEmptyBodyNormalize2NlNl(c *C) {
	a, err := asserts.Decode([]byte(exampleEmptyBody2NlNl))
	c.Assert(err, IsNil)
	c.Check(a.Type(), Equals, asserts.TestOnlyType)
	c.Check(a.Revision(), Equals, 0)
	c.Check(a.Format(), Equals, 0)
	c.Check(a.Body(), IsNil)
}

const exampleBodyAndExtraHeaders = "type: test-only\n" +
	"format: 1\n" +
	"authority-id: auth-id2\n" +
	"primary-key: abc\n" +
	"revision: 5\n" +
	"header1: value1\n" +
	"header2: value2\n" +
	"body-length: 8\n" +
	"sign-key-sha3-384: Jv8_JiHiIzJVcO9M55pPdqSDWUvuhfDIBJUS-3VW7F_idjix7Ffn5qMxB21ZQuij\n\n" +
	"THE-BODY" +
	"\n\n" +
	"AXNpZw==\n"

func (as *assertsSuite) TestDecodeWithABodyAndExtraHeaders(c *C) {
	a, err := asserts.Decode([]byte(exampleBodyAndExtraHeaders))
	c.Assert(err, IsNil)
	c.Check(a.Type(), Equals, asserts.TestOnlyType)
	c.Check(a.AuthorityID(), Equals, "auth-id2")
	c.Check(a.SignKeyID(), Equals, exKeyID)
	c.Check(a.Header("primary-key"), Equals, "abc")
	c.Check(a.Revision(), Equals, 5)
	c.Check(a.Format(), Equals, 1)
	c.Check(a.SupportedFormat(), Equals, true)
	c.Check(a.Header("header1"), Equals, "value1")
	c.Check(a.Header("header2"), Equals, "value2")
	c.Check(a.Body(), DeepEquals, []byte("THE-BODY"))

}

const exampleUnsupportedFormat = "type: test-only\n" +
	"format: 77\n" +
	"authority-id: auth-id2\n" +
	"primary-key: abc\n" +
	"revision: 5\n" +
	"header1: value1\n" +
	"header2: value2\n" +
	"sign-key-sha3-384: Jv8_JiHiIzJVcO9M55pPdqSDWUvuhfDIBJUS-3VW7F_idjix7Ffn5qMxB21ZQuij\n\n" +
	"AXNpZw==\n"

func (as *assertsSuite) TestDecodeUnsupportedFormat(c *C) {
	a, err := asserts.Decode([]byte(exampleUnsupportedFormat))
	c.Assert(err, IsNil)
	c.Check(a.Type(), Equals, asserts.TestOnlyType)
	c.Check(a.AuthorityID(), Equals, "auth-id2")
	c.Check(a.SignKeyID(), Equals, exKeyID)
	c.Check(a.Header("primary-key"), Equals, "abc")
	c.Check(a.Revision(), Equals, 5)
	c.Check(a.Format(), Equals, 77)
	c.Check(a.SupportedFormat(), Equals, false)
}

func (as *assertsSuite) TestDecodeGetSignatureBits(c *C) {
	content := "type: test-only\n" +
		"authority-id: auth-id1\n" +
		"primary-key: xyz\n" +
		"revision: 5\n" +
		"header1: value1\n" +
		"body-length: 8\n" +
		"sign-key-sha3-384: Jv8_JiHiIzJVcO9M55pPdqSDWUvuhfDIBJUS-3VW7F_idjix7Ffn5qMxB21ZQuij\n\n" +
		"THE-BODY"
	encoded := content +
		"\n\n" +
		"AXNpZw=="
	a, err := asserts.Decode([]byte(encoded))
	c.Assert(err, IsNil)
	c.Check(a.Type(), Equals, asserts.TestOnlyType)
	c.Check(a.AuthorityID(), Equals, "auth-id1")
	c.Check(a.SignKeyID(), Equals, exKeyID)
	cont, signature := a.Signature()
	c.Check(signature, DeepEquals, []byte("AXNpZw=="))
	c.Check(cont, DeepEquals, []byte(content))
}

func (as *assertsSuite) TestDecodeNoSignatureSplit(c *C) {
	for _, encoded := range []string{"", "foo"} {
		_, err := asserts.Decode([]byte(encoded))
		c.Check(err, ErrorMatches, "assertion content/signature separator not found")
	}
}

func (as *assertsSuite) TestDecodeHeaderParsingErrors(c *C) {
	headerParsingErrorsTests := []struct{ encoded, expectedErr string }{
		{string([]byte{255, '\n', '\n'}), "header is not utf8"},
		{"foo: a\nbar\n\n", `header entry missing ':' separator: "bar"`},
		{"TYPE: foo\n\n", `invalid header name: "TYPE"`},
		{"foo: a\nbar:>\n\n", `header entry should have a space or newline \(for multiline\) before value: "bar:>"`},
		{"foo: a\nbar:\n\n", `expected 4 chars nesting prefix after multiline introduction "bar:": EOF`},
		{"foo: a\nbar:\nbaz: x\n\n", `expected 4 chars nesting prefix after multiline introduction "bar:": "baz: x"`},
		{"foo: a:\nbar: b\nfoo: x\n\n", `repeated header: "foo"`},
	}

	for _, test := range headerParsingErrorsTests {
		_, err := asserts.Decode([]byte(test.encoded))
		c.Check(err, ErrorMatches, "parsing assertion headers: "+test.expectedErr)
	}
}

func (as *assertsSuite) TestDecodeInvalid(c *C) {
	keyIDHdr := "sign-key-sha3-384: Jv8_JiHiIzJVcO9M55pPdqSDWUvuhfDIBJUS-3VW7F_idjix7Ffn5qMxB21ZQuij\n"
	encoded := "type: test-only\n" +
		"format: 0\n" +
		"authority-id: auth-id\n" +
		"primary-key: abc\n" +
		"revision: 0\n" +
		"body-length: 5\n" +
		keyIDHdr +
		"\n" +
		"abcde" +
		"\n\n" +
		"AXNpZw=="

	invalidAssertTests := []struct{ original, invalid, expectedErr string }{
		{"body-length: 5", "body-length: z", `assertion: "body-length" header is not an integer: z`},
		{"body-length: 5", "body-length: 3", "assertion body length and declared body-length don't match: 5 != 3"},
		{"authority-id: auth-id\n", "", `assertion: "authority-id" header is mandatory`},
		{"authority-id: auth-id\n", "authority-id: \n", `assertion: "authority-id" header should not be empty`},
		{keyIDHdr, "", `assertion: "sign-key-sha3-384" header is mandatory`},
		{keyIDHdr, "sign-key-sha3-384: \n", `assertion: "sign-key-sha3-384" header should not be empty`},
		{keyIDHdr, "sign-key-sha3-384: $\n", `assertion: "sign-key-sha3-384" header cannot be decoded: .*`},
		{keyIDHdr, "sign-key-sha3-384: eHl6\n", `assertion: "sign-key-sha3-384" header does not have the expected bit length: 24`},
		{"AXNpZw==", "", "empty assertion signature"},
		{"type: test-only\n", "", `assertion: "type" header is mandatory`},
		{"type: test-only\n", "type: unknown\n", `unknown assertion type: "unknown"`},
		{"revision: 0\n", "revision: Z\n", `assertion: "revision" header is not an integer: Z`},
		{"revision: 0\n", "revision:\n  - 1\n", `assertion: "revision" header is not an integer: \[1\]`},
		{"revision: 0\n", "revision: -10\n", "assertion: revision should be positive: -10"},
		{"format: 0\n", "format: Z\n", `assertion: "format" header is not an integer: Z`},
		{"format: 0\n", "format: -10\n", "assertion: format should be positive: -10"},
		{"primary-key: abc\n", "", `assertion test-only: "primary-key" header is mandatory`},
		{"primary-key: abc\n", "primary-key:\n  - abc\n", `assertion test-only: "primary-key" header must be a string`},
		{"primary-key: abc\n", "primary-key: a/c\n", `assertion test-only: "primary-key" primary key header cannot contain '/'`},
		{"abcde", "ab\xffde", "body is not utf8"},
	}

	for _, test := range invalidAssertTests {
		invalid := strings.Replace(encoded, test.original, test.invalid, 1)
		_, err := asserts.Decode([]byte(invalid))
		c.Check(err, ErrorMatches, test.expectedErr)
	}
}

func (as *assertsSuite) TestDecodeNoAuthorityInvalid(c *C) {
	invalid := "type: test-only-no-authority\n" +
		"authority-id: auth-id1\n" +
		"hdr: FOO\n" +
		"sign-key-sha3-384: Jv8_JiHiIzJVcO9M55pPdqSDWUvuhfDIBJUS-3VW7F_idjix7Ffn5qMxB21ZQuij" +
		"\n\n" +
		"openpgp c2ln"

	_, err := asserts.Decode([]byte(invalid))
	c.Check(err, ErrorMatches, `"test-only-no-authority" assertion cannot have authority-id set`)
}

func checkContent(c *C, a asserts.Assertion, encoded string) {
	expected, err := asserts.Decode([]byte(encoded))
	c.Assert(err, IsNil)
	expectedCont, _ := expected.Signature()

	cont, _ := a.Signature()
	c.Check(cont, DeepEquals, expectedCont)
}

func (as *assertsSuite) TestEncoderDecoderHappy(c *C) {
	stream := new(bytes.Buffer)
	enc := asserts.NewEncoder(stream)
	enc.WriteEncoded([]byte(exampleEmptyBody2NlNl))
	enc.WriteEncoded([]byte(exampleBodyAndExtraHeaders))
	enc.WriteEncoded([]byte(exampleEmptyBodyAllDefaults))

	decoder := asserts.NewDecoder(stream)
	a, err := decoder.Decode()
	c.Assert(err, IsNil)
	c.Check(a.Type(), Equals, asserts.TestOnlyType)
	_, ok := a.(*asserts.TestOnly)
	c.Check(ok, Equals, true)
	checkContent(c, a, exampleEmptyBody2NlNl)

	a, err = decoder.Decode()
	c.Assert(err, IsNil)
	checkContent(c, a, exampleBodyAndExtraHeaders)

	a, err = decoder.Decode()
	c.Assert(err, IsNil)
	checkContent(c, a, exampleEmptyBodyAllDefaults)

	a, err = decoder.Decode()
	c.Assert(err, Equals, io.EOF)
}

func (as *assertsSuite) TestDecodeEmptyStream(c *C) {
	stream := new(bytes.Buffer)
	decoder := asserts.NewDecoder(stream)
	_, err := decoder.Decode()
	c.Check(err, Equals, io.EOF)
}

func (as *assertsSuite) TestDecoderHappyWithSeparatorsVariations(c *C) {
	streams := []string{
		exampleBodyAndExtraHeaders,
		exampleEmptyBody2NlNl,
		exampleEmptyBodyAllDefaults,
	}

	for _, streamData := range streams {
		stream := bytes.NewBufferString(streamData)
		decoder := asserts.NewDecoderStressed(stream, 16, 1024, 1024, 1024)
		a, err := decoder.Decode()
		c.Assert(err, IsNil, Commentf("stream: %q", streamData))

		checkContent(c, a, streamData)

		a, err = decoder.Decode()
		c.Check(a, IsNil)
		c.Check(err, Equals, io.EOF, Commentf("stream: %q", streamData))
	}
}

func (as *assertsSuite) TestDecoderHappyWithTrailerDoubleNewlines(c *C) {
	streams := []string{
		exampleBodyAndExtraHeaders,
		exampleEmptyBody2NlNl,
		exampleEmptyBodyAllDefaults,
	}

	for _, streamData := range streams {
		stream := bytes.NewBufferString(streamData)
		if strings.HasSuffix(streamData, "\n") {
			stream.WriteString("\n")
		} else {
			stream.WriteString("\n\n")
		}

		decoder := asserts.NewDecoderStressed(stream, 16, 1024, 1024, 1024)
		a, err := decoder.Decode()
		c.Assert(err, IsNil, Commentf("stream: %q", streamData))

		checkContent(c, a, streamData)

		a, err = decoder.Decode()
		c.Check(a, IsNil)
		c.Check(err, Equals, io.EOF, Commentf("stream: %q", streamData))
	}
}

func (as *assertsSuite) TestDecoderUnexpectedEOF(c *C) {
	streamData := exampleBodyAndExtraHeaders + "\n" + exampleEmptyBodyAllDefaults
	fstHeadEnd := strings.Index(exampleBodyAndExtraHeaders, "\n\n")
	sndHeadEnd := len(exampleBodyAndExtraHeaders) + 1 + strings.Index(exampleEmptyBodyAllDefaults, "\n\n")

	for _, brk := range []int{1, fstHeadEnd / 2, fstHeadEnd, fstHeadEnd + 1, fstHeadEnd + 2, fstHeadEnd + 6} {
		stream := bytes.NewBufferString(streamData[:brk])
		decoder := asserts.NewDecoderStressed(stream, 16, 1024, 1024, 1024)
		_, err := decoder.Decode()
		c.Check(err, Equals, io.ErrUnexpectedEOF, Commentf("brk: %d", brk))
	}

	for _, brk := range []int{sndHeadEnd, sndHeadEnd + 1} {
		stream := bytes.NewBufferString(streamData[:brk])
		decoder := asserts.NewDecoder(stream)
		_, err := decoder.Decode()
		c.Assert(err, IsNil)

		_, err = decoder.Decode()
		c.Check(err, Equals, io.ErrUnexpectedEOF, Commentf("brk: %d", brk))
	}
}

func (as *assertsSuite) TestDecoderBrokenBodySeparation(c *C) {
	streamData := strings.Replace(exampleBodyAndExtraHeaders, "THE-BODY\n\n", "THE-BODY", 1)
	decoder := asserts.NewDecoder(bytes.NewBufferString(streamData))
	_, err := decoder.Decode()
	c.Assert(err, ErrorMatches, "missing content/signature separator")

	streamData = strings.Replace(exampleBodyAndExtraHeaders, "THE-BODY\n\n", "THE-BODY\n", 1)
	decoder = asserts.NewDecoder(bytes.NewBufferString(streamData))
	_, err = decoder.Decode()
	c.Assert(err, ErrorMatches, "missing content/signature separator")
}

func (as *assertsSuite) TestDecoderHeadTooBig(c *C) {
	decoder := asserts.NewDecoderStressed(bytes.NewBufferString(exampleBodyAndExtraHeaders), 4, 4, 1024, 1024)
	_, err := decoder.Decode()
	c.Assert(err, ErrorMatches, `error reading assertion headers: maximum size exceeded while looking for delimiter "\\n\\n"`)
}

func (as *assertsSuite) TestDecoderBodyTooBig(c *C) {
	decoder := asserts.NewDecoderStressed(bytes.NewBufferString(exampleBodyAndExtraHeaders), 1024, 1024, 5, 1024)
	_, err := decoder.Decode()
	c.Assert(err, ErrorMatches, "assertion body length 8 exceeds maximum body size")
}

func (as *assertsSuite) TestDecoderSignatureTooBig(c *C) {
	decoder := asserts.NewDecoderStressed(bytes.NewBufferString(exampleBodyAndExtraHeaders), 4, 1024, 1024, 7)
	_, err := decoder.Decode()
	c.Assert(err, ErrorMatches, `error reading assertion signature: maximum size exceeded while looking for delimiter "\\n\\n"`)
}

func (as *assertsSuite) TestDecoderDefaultMaxBodySize(c *C) {
	enc := strings.Replace(exampleBodyAndExtraHeaders, "body-length: 8", "body-length: 2097153", 1)
	decoder := asserts.NewDecoder(bytes.NewBufferString(enc))
	_, err := decoder.Decode()
	c.Assert(err, ErrorMatches, "assertion body length 2097153 exceeds maximum body size")
}

func (as *assertsSuite) TestDecoderWithTypeMaxBodySize(c *C) {
	ex1 := strings.Replace(exampleBodyAndExtraHeaders, "body-length: 8", "body-length: 2097152", 1)
	ex1 = strings.Replace(ex1, "THE-BODY", strings.Repeat("B", 2*1024*1024), 1)
	ex1toobig := strings.Replace(exampleBodyAndExtraHeaders, "body-length: 8", "body-length: 2097153", 1)
	ex1toobig = strings.Replace(ex1toobig, "THE-BODY", strings.Repeat("B", 2*1024*1024+1), 1)
	const ex2 = `type: test-only-2
authority-id: auth-id1
pk1: foo
pk2: bar
body-length: 3
sign-key-sha3-384: Jv8_JiHiIzJVcO9M55pPdqSDWUvuhfDIBJUS-3VW7F_idjix7Ffn5qMxB21ZQuij

XYZ

AXNpZw==`

	decoder := asserts.NewDecoderWithTypeMaxBodySize(bytes.NewBufferString(ex1+"\n"+ex2), map[*asserts.AssertionType]int{
		asserts.TestOnly2Type: 3,
	})
	a1, err := decoder.Decode()
	c.Assert(err, IsNil)
	c.Check(a1.Body(), HasLen, 2*1024*1024)
	a2, err := decoder.Decode()
	c.Assert(err, IsNil)
	c.Check(a2.Body(), DeepEquals, []byte("XYZ"))

	decoder = asserts.NewDecoderWithTypeMaxBodySize(bytes.NewBufferString(ex1+"\n"+ex2), map[*asserts.AssertionType]int{
		asserts.TestOnly2Type: 2,
	})
	a1, err = decoder.Decode()
	c.Assert(err, IsNil)
	c.Check(a1.Body(), HasLen, 2*1024*1024)
	_, err = decoder.Decode()
	c.Assert(err, ErrorMatches, `assertion body length 3 exceeds maximum body size 2 for "test-only-2" assertions`)

	decoder = asserts.NewDecoderWithTypeMaxBodySize(bytes.NewBufferString(ex2+"\n\n"+ex1toobig), map[*asserts.AssertionType]int{
		asserts.TestOnly2Type: 3,
	})
	a2, err = decoder.Decode()
	c.Assert(err, IsNil)
	c.Check(a2.Body(), DeepEquals, []byte("XYZ"))
	_, err = decoder.Decode()
	c.Assert(err, ErrorMatches, "assertion body length 2097153 exceeds maximum body size")
}

func (as *assertsSuite) TestEncode(c *C) {
	encoded := []byte("type: test-only\n" +
		"authority-id: auth-id2\n" +
		"primary-key: xyz\n" +
		"revision: 5\n" +
		"header1: value1\n" +
		"header2: value2\n" +
		"body-length: 8\n" +
		"sign-key-sha3-384: Jv8_JiHiIzJVcO9M55pPdqSDWUvuhfDIBJUS-3VW7F_idjix7Ffn5qMxB21ZQuij\n\n" +
		"THE-BODY" +
		"\n\n" +
		"AXNpZw==")
	a, err := asserts.Decode(encoded)
	c.Assert(err, IsNil)
	encodeRes := asserts.Encode(a)
	c.Check(encodeRes, DeepEquals, encoded)
}

func (as *assertsSuite) TestEncoderOK(c *C) {
	encoded := []byte("type: test-only\n" +
		"authority-id: auth-id2\n" +
		"primary-key: xyzyz\n" +
		"revision: 5\n" +
		"header1: value1\n" +
		"header2: value2\n" +
		"body-length: 8\n" +
		"sign-key-sha3-384: Jv8_JiHiIzJVcO9M55pPdqSDWUvuhfDIBJUS-3VW7F_idjix7Ffn5qMxB21ZQuij\n\n" +
		"THE-BODY" +
		"\n\n" +
		"AXNpZw==")
	a0, err := asserts.Decode(encoded)
	c.Assert(err, IsNil)
	cont0, _ := a0.Signature()

	stream := new(bytes.Buffer)
	enc := asserts.NewEncoder(stream)
	enc.Encode(a0)

	c.Check(bytes.HasSuffix(stream.Bytes(), []byte{'\n'}), Equals, true)

	dec := asserts.NewDecoder(stream)
	a1, err := dec.Decode()
	c.Assert(err, IsNil)

	cont1, _ := a1.Signature()
	c.Check(cont1, DeepEquals, cont0)
}

func (as *assertsSuite) TestEncoderSingleDecodeOK(c *C) {
	encoded := []byte("type: test-only\n" +
		"authority-id: auth-id2\n" +
		"primary-key: abc\n" +
		"revision: 5\n" +
		"header1: value1\n" +
		"header2: value2\n" +
		"body-length: 8\n" +
		"sign-key-sha3-384: Jv8_JiHiIzJVcO9M55pPdqSDWUvuhfDIBJUS-3VW7F_idjix7Ffn5qMxB21ZQuij\n\n" +
		"THE-BODY" +
		"\n\n" +
		"AXNpZw==")
	a0, err := asserts.Decode(encoded)
	c.Assert(err, IsNil)
	cont0, _ := a0.Signature()

	stream := new(bytes.Buffer)
	enc := asserts.NewEncoder(stream)
	enc.Encode(a0)

	a1, err := asserts.Decode(stream.Bytes())
	c.Assert(err, IsNil)

	cont1, _ := a1.Signature()
	c.Check(cont1, DeepEquals, cont0)
}

func (as *assertsSuite) TestSignFormatSanityEmptyBody(c *C) {
	headers := map[string]interface{}{
		"authority-id": "auth-id1",
		"primary-key":  "0",
	}
	a, err := asserts.AssembleAndSignInTest(asserts.TestOnlyType, headers, nil, testPrivKey1)
	c.Assert(err, IsNil)

	_, err = asserts.Decode(asserts.Encode(a))
	c.Check(err, IsNil)
}

func (as *assertsSuite) TestSignFormatSanityNonEmptyBody(c *C) {
	headers := map[string]interface{}{
		"authority-id": "auth-id1",
		"primary-key":  "0",
	}
	body := []byte("THE-BODY")
	a, err := asserts.AssembleAndSignInTest(asserts.TestOnlyType, headers, body, testPrivKey1)
	c.Assert(err, IsNil)
	c.Check(a.Body(), DeepEquals, body)

	decoded, err := asserts.Decode(asserts.Encode(a))
	c.Assert(err, IsNil)
	c.Check(decoded.Body(), DeepEquals, body)
}

func (as *assertsSuite) TestSignFormatSanitySupportMultilineHeaderValues(c *C) {
	headers := map[string]interface{}{
		"authority-id": "auth-id1",
		"primary-key":  "0",
	}

	multilineVals := []string{
		"a\n",
		"\na",
		"a\n\b\nc",
		"a\n\b\nc\n",
		"\na\n",
		"\n\na\n\nb\n\nc",
	}

	for _, multilineVal := range multilineVals {
		headers["multiline"] = multilineVal
		if len(multilineVal)%2 == 1 {
			headers["odd"] = "true"
		}

		a, err := asserts.AssembleAndSignInTest(asserts.TestOnlyType, headers, nil, testPrivKey1)
		c.Assert(err, IsNil)

		decoded, err := asserts.Decode(asserts.Encode(a))
		c.Assert(err, IsNil)

		c.Check(decoded.Header("multiline"), Equals, multilineVal)
	}
}

func (as *assertsSuite) TestSignFormatAndRevision(c *C) {
	headers := map[string]interface{}{
		"authority-id": "auth-id1",
		"primary-key":  "0",
		"format":       "1",
		"revision":     "11",
	}

	a, err := asserts.AssembleAndSignInTest(asserts.TestOnlyType, headers, nil, testPrivKey1)
	c.Assert(err, IsNil)

	c.Check(a.Revision(), Equals, 11)
	c.Check(a.Format(), Equals, 1)
	c.Check(a.SupportedFormat(), Equals, true)

	a1, err := asserts.Decode(asserts.Encode(a))
	c.Assert(err, IsNil)

	c.Check(a1.Revision(), Equals, 11)
	c.Check(a1.Format(), Equals, 1)
	c.Check(a1.SupportedFormat(), Equals, true)
}

func (as *assertsSuite) TestSignBodyIsUTF8Text(c *C) {
	headers := map[string]interface{}{
		"authority-id": "auth-id1",
		"primary-key":  "0",
	}
	_, err := asserts.AssembleAndSignInTest(asserts.TestOnlyType, headers, []byte{'\xff'}, testPrivKey1)
	c.Assert(err, ErrorMatches, "assertion body is not utf8")
}

func (as *assertsSuite) TestHeaders(c *C) {
	encoded := []byte("type: test-only\n" +
		"authority-id: auth-id2\n" +
		"primary-key: abc\n" +
		"revision: 5\n" +
		"header1: value1\n" +
		"header2: value2\n" +
		"body-length: 8\n" +
		"sign-key-sha3-384: Jv8_JiHiIzJVcO9M55pPdqSDWUvuhfDIBJUS-3VW7F_idjix7Ffn5qMxB21ZQuij\n\n" +
		"THE-BODY" +
		"\n\n" +
		"AXNpZw==")
	a, err := asserts.Decode(encoded)
	c.Assert(err, IsNil)

	hs := a.Headers()
	c.Check(hs, DeepEquals, map[string]interface{}{
		"type":              "test-only",
		"authority-id":      "auth-id2",
		"primary-key":       "abc",
		"revision":          "5",
		"header1":           "value1",
		"header2":           "value2",
		"body-length":       "8",
		"sign-key-sha3-384": exKeyID,
	})
}

func (as *assertsSuite) TestHeadersReturnsCopy(c *C) {
	encoded := []byte("type: test-only\n" +
		"authority-id: auth-id2\n" +
		"primary-key: xyz\n" +
		"revision: 5\n" +
		"header1: value1\n" +
		"header2: value2\n" +
		"body-length: 8\n" +
		"sign-key-sha3-384: Jv8_JiHiIzJVcO9M55pPdqSDWUvuhfDIBJUS-3VW7F_idjix7Ffn5qMxB21ZQuij\n\n" +
		"THE-BODY" +
		"\n\n" +
		"AXNpZw==")
	a, err := asserts.Decode(encoded)
	c.Assert(err, IsNil)

	hs := a.Headers()
	// casual later result mutation doesn't trip us
	delete(hs, "primary-key")
	c.Check(a.Header("primary-key"), Equals, "xyz")
}

func (as *assertsSuite) TestAssembleRoundtrip(c *C) {
	encoded := []byte("type: test-only\n" +
		"format: 1\n" +
		"authority-id: auth-id2\n" +
		"primary-key: abc\n" +
		"revision: 5\n" +
		"header1: value1\n" +
		"header2: value2\n" +
		"body-length: 8\n" +
		"sign-key-sha3-384: Jv8_JiHiIzJVcO9M55pPdqSDWUvuhfDIBJUS-3VW7F_idjix7Ffn5qMxB21ZQuij\n\n" +
		"THE-BODY" +
		"\n\n" +
		"AXNpZw==")
	a, err := asserts.Decode(encoded)
	c.Assert(err, IsNil)

	cont, sig := a.Signature()
	reassembled, err := asserts.Assemble(a.Headers(), a.Body(), cont, sig)
	c.Assert(err, IsNil)

	c.Check(reassembled.Headers(), DeepEquals, a.Headers())
	c.Check(reassembled.Body(), DeepEquals, a.Body())

	reassembledEncoded := asserts.Encode(reassembled)
	c.Check(reassembledEncoded, DeepEquals, encoded)
}

func (as *assertsSuite) TestSignKeyID(c *C) {
	headers := map[string]interface{}{
		"authority-id": "auth-id1",
		"primary-key":  "0",
	}
	a, err := asserts.AssembleAndSignInTest(asserts.TestOnlyType, headers, nil, testPrivKey1)
	c.Assert(err, IsNil)

	keyID := a.SignKeyID()
	c.Check(keyID, Equals, testPrivKey1.PublicKey().ID())
}

func (as *assertsSuite) TestSelfRef(c *C) {
	headers := map[string]interface{}{
		"authority-id": "auth-id1",
		"primary-key":  "0",
	}
	a1, err := asserts.AssembleAndSignInTest(asserts.TestOnlyType, headers, nil, testPrivKey1)
	c.Assert(err, IsNil)

	c.Check(a1.Ref(), DeepEquals, &asserts.Ref{
		Type:       asserts.TestOnlyType,
		PrimaryKey: []string{"0"},
	})

	headers = map[string]interface{}{
		"authority-id": "auth-id1",
		"pk1":          "a",
		"pk2":          "b",
	}
	a2, err := asserts.AssembleAndSignInTest(asserts.TestOnly2Type, headers, nil, testPrivKey1)
	c.Assert(err, IsNil)

	c.Check(a2.Ref(), DeepEquals, &asserts.Ref{
		Type:       asserts.TestOnly2Type,
		PrimaryKey: []string{"a", "b"},
	})
}

func (as *assertsSuite) TestAssembleHeadersCheck(c *C) {
	cont := []byte("type: test-only\n" +
		"authority-id: auth-id2\n" +
		"primary-key: abc\n" +
		"revision: 5")
	headers := map[string]interface{}{
		"type":         "test-only",
		"authority-id": "auth-id2",
		"primary-key":  "abc",
		"revision":     5, // must be a string actually!
	}

	_, err := asserts.Assemble(headers, nil, cont, nil)
	c.Check(err, ErrorMatches, `header "revision": header values must be strings or nested lists or maps with strings as the only scalars: 5`)
}

func (as *assertsSuite) TestSignWithoutAuthorityMisuse(c *C) {
	_, err := asserts.SignWithoutAuthority(asserts.TestOnlyType, nil, nil, testPrivKey1)
	c.Check(err, ErrorMatches, `cannot sign assertions needing a definite authority with SignWithoutAuthority`)

	_, err = asserts.SignWithoutAuthority(asserts.TestOnlyNoAuthorityType,
		map[string]interface{}{
			"authority-id": "auth-id1",
			"hdr":          "FOO",
		}, nil, testPrivKey1)
	c.Check(err, ErrorMatches, `"test-only-no-authority" assertion cannot have authority-id set`)
}

func (ss *serialSuite) TestSignatureCheckError(c *C) {
	sreq, err := asserts.SignWithoutAuthority(asserts.TestOnlyNoAuthorityType,
		map[string]interface{}{
			"hdr": "FOO",
		}, nil, testPrivKey1)
	c.Assert(err, IsNil)

	err = asserts.SignatureCheck(sreq, testPrivKey2.PublicKey())
	c.Check(err, ErrorMatches, `failed signature verification:.*`)
}

func (as *assertsSuite) TestWithAuthority(c *C) {
	withAuthority := []string{
		"account",
		"account-key",
		"base-declaration",
		"store",
		"snap-declaration",
		"snap-build",
		"snap-revision",
		"snap-developer",
		"model",
		"serial",
		"system-user",
		"validation",
		"repair",
	}
	c.Check(withAuthority, HasLen, asserts.NumAssertionType-3) // excluding device-session-request, serial-request, account-key-request
	for _, name := range withAuthority {
		typ := asserts.Type(name)
		_, err := asserts.AssembleAndSignInTest(typ, nil, nil, testPrivKey1)
		c.Check(err, ErrorMatches, `"authority-id" header is mandatory`)
	}
}
