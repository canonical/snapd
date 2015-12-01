// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2015 Canonical Ltd
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
	"strings"

	. "gopkg.in/check.v1"

	"github.com/ubuntu-core/snappy/asserts"
)

type assertsSuite struct{}

var _ = Suite(&assertsSuite{})

func (as *assertsSuite) TestDecodeEmptyBodyAllDefaults(c *C) {
	encoded := "type: test-only\n" +
		"authority-id: auth-id1" +
		"\n\n" +
		"openpgp c2ln"
	a, err := asserts.Decode([]byte(encoded))
	c.Assert(err, IsNil)
	c.Check(a.Type(), Equals, asserts.AssertionType("test-only"))
	_, ok := a.(*asserts.TestOnly)
	c.Check(ok, Equals, true)
	c.Check(a.Revision(), Equals, 0)
	c.Check(a.Body(), IsNil)
	c.Check(a.Header("header1"), Equals, "")
	c.Check(a.AuthorityID(), Equals, "auth-id1")
}

func (as *assertsSuite) TestDecodeEmptyBodyNormalize2NlNl(c *C) {
	encoded := "type: test-only\n" +
		"authority-id: auth-id1\n" +
		"revision: 0\n" +
		"body-length: 0" +
		"\n\n" +
		"\n\n" +
		"openpgp c2ln"
	a, err := asserts.Decode([]byte(encoded))
	c.Assert(err, IsNil)
	c.Check(a.Type(), Equals, asserts.AssertionType("test-only"))
	c.Check(a.Revision(), Equals, 0)
	c.Check(a.Body(), IsNil)
}

func (as *assertsSuite) TestDecodeWithABodyAndExtraHeaders(c *C) {
	encoded := "type: test-only\n" +
		"authority-id: auth-id2\n" +
		"primary-key1: key1\n" +
		"primary-key2: key2\n" +
		"revision: 5\n" +
		"header1: value1\n" +
		"header2: value2\n" +
		"body-length: 8\n\n" +
		"THE-BODY" +
		"\n\n" +
		"openpgp c2ln"
	a, err := asserts.Decode([]byte(encoded))
	c.Assert(err, IsNil)
	c.Check(a.Type(), Equals, asserts.AssertionType("test-only"))
	c.Check(a.AuthorityID(), Equals, "auth-id2")
	c.Check(a.Header("primary-key1"), Equals, "key1")
	c.Check(a.Header("primary-key2"), Equals, "key2")
	c.Check(a.Revision(), Equals, 5)
	c.Check(a.Header("header1"), Equals, "value1")
	c.Check(a.Header("header2"), Equals, "value2")
	c.Check(a.Body(), DeepEquals, []byte("THE-BODY"))

}

func (as *assertsSuite) TestDecodeGetSignatureBits(c *C) {
	content := "type: test-only\n" +
		"authority-id: auth-id1\n" +
		"revision: 5\n" +
		"header1: value1\n" +
		"body-length: 8\n\n" +
		"THE-BODY"
	encoded := content +
		"\n\n" +
		"openpgp c2ln"
	a, err := asserts.Decode([]byte(encoded))
	c.Assert(err, IsNil)
	c.Check(a.Type(), Equals, asserts.AssertionType("test-only"))
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
		{"foo: a\nbar\n\n", "header entry missing name value ': ' separation: \"bar\""},
		{"TYPE: foo\n\n", `invalid header name: "TYPE"`},
	}

	for _, test := range headerParsingErrorsTests {
		_, err := asserts.Decode([]byte(test.encoded))
		c.Check(err, ErrorMatches, "parsing assertion headers: "+test.expectedErr)
	}
}

func (as *assertsSuite) TestDecodeInvalid(c *C) {
	encoded := "type: test-only\n" +
		"authority-id: auth-id\n" +
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
		{"type: test-only\n", "type: unknown\n", "unknown assertion type: unknown"},
		{"revision: 0\n", "revision: Z\n", `assertion: "revision" header is not an integer: Z`},
		{"revision: 0\n", "revision: -10\n", "assertion: revision should be positive: -10"},
	}

	for _, test := range invalidAssertTests {
		invalid := strings.Replace(encoded, test.original, test.invalid, 1)
		_, err := asserts.Decode([]byte(invalid))
		c.Check(err, ErrorMatches, test.expectedErr)
	}
}

func (as *assertsSuite) TestEncode(c *C) {
	encoded := []byte("type: test-only\n" +
		"authority-id: auth-id2\n" +
		"primary-key1: key1\n" +
		"primary-key2: key2\n" +
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

func (as *assertsSuite) TestSignFormatSanityEmptyBody(c *C) {
	headers := map[string]string{
		"authority-id": "auth-id1",
		"primary-key":  "0",
	}
	a, err := asserts.BuildAndSignInTest(asserts.AssertionType("test-only"), headers, nil, asserts.WrapPrivateKey(testPrivKey1))
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
	a, err := asserts.BuildAndSignInTest(asserts.AssertionType("test-only"), headers, body, asserts.WrapPrivateKey(testPrivKey1))
	c.Assert(err, IsNil)
	c.Check(a.Body(), DeepEquals, body)

	decoded, err := asserts.Decode(asserts.Encode(a))
	c.Assert(err, IsNil)
	c.Check(decoded.Body(), DeepEquals, body)
}
