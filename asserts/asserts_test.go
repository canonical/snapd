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

	"github.com/ubuntu-core/snappy/asserts"
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

const exampleEmptyBodyAllDefaults = "type: test-only\n" +
	"authority-id: auth-id1\n" +
	"primary-key: abc" +
	"\n\n" +
	"openpgp c2ln"

func (as *assertsSuite) TestDecodeEmptyBodyAllDefaults(c *C) {
	a, err := asserts.Decode([]byte(exampleEmptyBodyAllDefaults))
	c.Assert(err, IsNil)
	c.Check(a.Type(), Equals, asserts.TestOnlyType)
	_, ok := a.(*asserts.TestOnly)
	c.Check(ok, Equals, true)
	c.Check(a.Revision(), Equals, 0)
	c.Check(a.Body(), IsNil)
	c.Check(a.Header("header1"), Equals, "")
	c.Check(a.AuthorityID(), Equals, "auth-id1")
}

const exampleEmptyBody2NlNl = "type: test-only\n" +
	"authority-id: auth-id1\n" +
	"primary-key: xyz\n" +
	"revision: 0\n" +
	"body-length: 0" +
	"\n\n" +
	"\n\n" +
	"openpgp c2ln\n"

func (as *assertsSuite) TestDecodeEmptyBodyNormalize2NlNl(c *C) {
	a, err := asserts.Decode([]byte(exampleEmptyBody2NlNl))
	c.Assert(err, IsNil)
	c.Check(a.Type(), Equals, asserts.TestOnlyType)
	c.Check(a.Revision(), Equals, 0)
	c.Check(a.Body(), IsNil)
}

const exampleBodyAndExtraHeaders = "type: test-only\n" +
	"authority-id: auth-id2\n" +
	"primary-key: abc\n" +
	"revision: 5\n" +
	"header1: value1\n" +
	"header2: value2\n" +
	"body-length: 8\n\n" +
	"THE-BODY" +
	"\n\n" +
	"openpgp c2ln\n"

func (as *assertsSuite) TestDecodeWithABodyAndExtraHeaders(c *C) {
	a, err := asserts.Decode([]byte(exampleBodyAndExtraHeaders))
	c.Assert(err, IsNil)
	c.Check(a.Type(), Equals, asserts.TestOnlyType)
	c.Check(a.AuthorityID(), Equals, "auth-id2")
	c.Check(a.Header("primary-key"), Equals, "abc")
	c.Check(a.Revision(), Equals, 5)
	c.Check(a.Header("header1"), Equals, "value1")
	c.Check(a.Header("header2"), Equals, "value2")
	c.Check(a.Body(), DeepEquals, []byte("THE-BODY"))

}

func (as *assertsSuite) TestDecodeGetSignatureBits(c *C) {
	content := "type: test-only\n" +
		"authority-id: auth-id1\n" +
		"primary-key: xyz\n" +
		"revision: 5\n" +
		"header1: value1\n" +
		"body-length: 8\n\n" +
		"THE-BODY"
	encoded := content +
		"\n\n" +
		"openpgp c2ln"
	a, err := asserts.Decode([]byte(encoded))
	c.Assert(err, IsNil)
	c.Check(a.Type(), Equals, asserts.TestOnlyType)
	c.Check(a.AuthorityID(), Equals, "auth-id1")
	cont, signature := a.Signature()
	c.Check(signature, DeepEquals, []byte("openpgp c2ln"))
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
		{"foo: a\nbar:>\n\n", `header entry should have a space or newline \(multiline\) before value: "bar:>"`},
		{"foo: a\nbar:\n\n", `empty multiline header value: "bar:"`},
		{"foo: a\nbar:\nbaz: x\n\n", `empty multiline header value: "bar:"`},
	}

	for _, test := range headerParsingErrorsTests {
		_, err := asserts.Decode([]byte(test.encoded))
		c.Check(err, ErrorMatches, "parsing assertion headers: "+test.expectedErr)
	}
}

func (as *assertsSuite) TestDecodeInvalid(c *C) {
	encoded := "type: test-only\n" +
		"authority-id: auth-id\n" +
		"primary-key: abc\n" +
		"revision: 0\n" +
		"body-length: 5" +
		"\n\n" +
		"abcde" +
		"\n\n" +
		"openpgp c2ln"

	invalidAssertTests := []struct{ original, invalid, expectedErr string }{
		{"body-length: 5", "body-length: z", `assertion: "body-length" header is not an integer: z`},
		{"body-length: 5", "body-length: 3", "assertion body length and declared body-length don't match: 5 != 3"},
		{"authority-id: auth-id\n", "", `assertion: "authority-id" header is mandatory`},
		{"authority-id: auth-id\n", "authority-id: \n", `assertion: "authority-id" header should not be empty`},
		{"openpgp c2ln", "", "empty assertion signature"},
		{"type: test-only\n", "", `assertion: "type" header is mandatory`},
		{"type: test-only\n", "type: unknown\n", `unknown assertion type: "unknown"`},
		{"revision: 0\n", "revision: Z\n", `assertion: "revision" header is not an integer: Z`},
		{"revision: 0\n", "revision: -10\n", "assertion: revision should be positive: -10"},
		{"primary-key: abc\n", "", `assertion test-only: "primary-key" header is mandatory`},
	}

	for _, test := range invalidAssertTests {
		invalid := strings.Replace(encoded, test.original, test.invalid, 1)
		_, err := asserts.Decode([]byte(invalid))
		c.Check(err, ErrorMatches, test.expectedErr)
	}
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
	asserts.EncoderAppend(enc, []byte(exampleEmptyBody2NlNl))
	asserts.EncoderAppend(enc, []byte(exampleBodyAndExtraHeaders))
	asserts.EncoderAppend(enc, []byte(exampleEmptyBodyAllDefaults))

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

func (as *assertsSuite) TestEncode(c *C) {
	encoded := []byte("type: test-only\n" +
		"authority-id: auth-id2\n" +
		"primary-key: xyz\n" +
		"revision: 5\n" +
		"header1: value1\n" +
		"header2: value2\n" +
		"body-length: 8\n\n" +
		"THE-BODY" +
		"\n\n" +
		"openpgp c2ln")
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
		"body-length: 8\n\n" +
		"THE-BODY" +
		"\n\n" +
		"openpgp c2ln")
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
		"body-length: 8\n\n" +
		"THE-BODY" +
		"\n\n" +
		"openpgp c2ln")
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
	headers := map[string]string{
		"authority-id": "auth-id1",
		"primary-key":  "0",
	}
	a, err := asserts.AssembleAndSignInTest(asserts.TestOnlyType, headers, nil, asserts.OpenPGPPrivateKey(testPrivKey1))
	c.Assert(err, IsNil)

	_, err = asserts.Decode(asserts.Encode(a))
	c.Check(err, IsNil)
}

func (as *assertsSuite) TestSignFormatSanityNonEmptyBody(c *C) {
	headers := map[string]string{
		"authority-id": "auth-id1",
		"primary-key":  "0",
	}
	body := []byte("THE-BODY")
	a, err := asserts.AssembleAndSignInTest(asserts.TestOnlyType, headers, body, asserts.OpenPGPPrivateKey(testPrivKey1))
	c.Assert(err, IsNil)
	c.Check(a.Body(), DeepEquals, body)

	decoded, err := asserts.Decode(asserts.Encode(a))
	c.Assert(err, IsNil)
	c.Check(decoded.Body(), DeepEquals, body)
}

func (as *assertsSuite) TestSignFormatSanitySupportMultilineHeaderValues(c *C) {
	headers := map[string]string{
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

		a, err := asserts.AssembleAndSignInTest(asserts.TestOnlyType, headers, nil, asserts.OpenPGPPrivateKey(testPrivKey1))
		c.Assert(err, IsNil)

		decoded, err := asserts.Decode(asserts.Encode(a))
		c.Assert(err, IsNil)

		c.Check(decoded.Header("multiline"), Equals, multilineVal)
	}
}

func (as *assertsSuite) TestHeaders(c *C) {
	encoded := []byte("type: test-only\n" +
		"authority-id: auth-id2\n" +
		"primary-key: abc\n" +
		"revision: 5\n" +
		"header1: value1\n" +
		"header2: value2\n" +
		"body-length: 8\n\n" +
		"THE-BODY" +
		"\n\n" +
		"openpgp c2ln")
	a, err := asserts.Decode(encoded)
	c.Assert(err, IsNil)

	hs := a.Headers()
	c.Check(hs, DeepEquals, map[string]string{
		"type":         "test-only",
		"authority-id": "auth-id2",
		"primary-key":  "abc",
		"revision":     "5",
		"header1":      "value1",
		"header2":      "value2",
		"body-length":  "8",
	})
}

func (as *assertsSuite) TestHeadersReturnsCopy(c *C) {
	encoded := []byte("type: test-only\n" +
		"authority-id: auth-id2\n" +
		"primary-key: xyz\n" +
		"revision: 5\n" +
		"header1: value1\n" +
		"header2: value2\n" +
		"body-length: 8\n\n" +
		"THE-BODY" +
		"\n\n" +
		"openpgp c2ln")
	a, err := asserts.Decode(encoded)
	c.Assert(err, IsNil)

	hs := a.Headers()
	// casual later result mutation doesn't trip us
	delete(hs, "primary-key")
	c.Check(a.Header("primary-key"), Equals, "xyz")
}

func (as *assertsSuite) TestAssembleRoundtrip(c *C) {
	encoded := []byte("type: test-only\n" +
		"authority-id: auth-id2\n" +
		"primary-key: abc\n" +
		"revision: 5\n" +
		"header1: value1\n" +
		"header2: value2\n" +
		"body-length: 8\n\n" +
		"THE-BODY" +
		"\n\n" +
		"openpgp c2ln")
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
