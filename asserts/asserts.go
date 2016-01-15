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

package asserts

import (
	"bytes"
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"
)

// AssertionType labels assertions of a given type
type AssertionType string

// Understood assertions
const (
	AccountKeyType   AssertionType = "account-key"
	SnapBuildType    AssertionType = "snap-build"
	SnapRevisionType AssertionType = "snap-revision"

// ...
)

// Assertion represents an assertion through its general elements.
type Assertion interface {
	// Type returns the type of this assertion
	Type() AssertionType
	// Revision returns the revision of this assertion
	Revision() int
	// AuthorityID returns the authority that signed this assertion
	AuthorityID() string

	// Header retrieves the header with name
	Header(name string) string

	// Headers returns the complete headers
	Headers() map[string]string

	// Body returns the body of this assertion
	Body() []byte

	// Signature returns the signed content and its unprocessed signature
	Signature() (content, signature []byte)
}

// assertionBase is the concrete base to hold representation data for actual assertions.
type assertionBase struct {
	headers map[string]string
	body    []byte
	// parsed revision
	revision int
	// preserved content
	content []byte
	// unprocessed signature
	signature []byte
}

// Type returns the assertion type.
func (ab *assertionBase) Type() AssertionType {
	return AssertionType(ab.headers["type"])
}

// Revision returns the assertion revision.
func (ab *assertionBase) Revision() int {
	return ab.revision
}

// AuthorityID returns the authority-id a.k.a the signer id of the assertion.
func (ab *assertionBase) AuthorityID() string {
	return ab.headers["authority-id"]
}

// Header returns the value of an header by name.
func (ab *assertionBase) Header(name string) string {
	return ab.headers[name]
}

// Headers returns the complete headers.
func (ab *assertionBase) Headers() map[string]string {
	res := make(map[string]string, len(ab.headers))
	for name, v := range ab.headers {
		res[name] = v
	}
	return res
}

// Body returns the body of the assertion.
func (ab *assertionBase) Body() []byte {
	return ab.body
}

// Signature returns the signed content and its unprocessed signature.
func (ab *assertionBase) Signature() (content, signature []byte) {
	return ab.content, ab.signature
}

// sanity check
var _ Assertion = (*assertionBase)(nil)

var (
	nlnl = []byte("\n\n")

	// for basic sanity checking of header names
	headerNameSanity = regexp.MustCompile("^[a-z][a-z0-9-]*[a-z0-9]$")
)

func parseHeaders(head []byte) (map[string]string, error) {
	if !utf8.Valid(head) {
		return nil, fmt.Errorf("header is not utf8")
	}
	headers := make(map[string]string)
	for _, entry := range strings.Split(string(head), "\n") {
		nameValueSplit := strings.Index(entry, ": ")
		if nameValueSplit == -1 {
			return nil, fmt.Errorf("header entry missing name value ': ' separation: %q", entry)
		}
		name := entry[:nameValueSplit]
		if !headerNameSanity.MatchString(name) {
			return nil, fmt.Errorf("invalid header name: %q", name)
		}
		headers[name] = entry[nameValueSplit+2:]
	}
	return headers, nil
}

// Decode parses a serialized assertion.
//
// The expected serialisation format looks like:
//
//   HEADER ("\n\n" BODY?)? "\n\n" SIGNATURE
//
// where:
//
//    HEADER is a set of header lines separated by "\n"
//    BODY can be arbitrary,
//    SIGNATURE is the signature
//
// A header line looks like:
//
//   NAME ": " VALUE
//
// The following headers are mandatory:
//
//   type
//   authority-id (the signer id)
//
// The following headers expect integer values and if omitted
// otherwise are assumed to be 0:
//
//   revision (a positive int)
//   body-length (expected to be equal to the length of BODY)
//
func Decode(serializedAssertion []byte) (Assertion, error) {
	// copy to get an independent backstorage that can't be mutated later
	assertionSnapshot := make([]byte, len(serializedAssertion))
	copy(assertionSnapshot, serializedAssertion)
	contentSignatureSplit := bytes.LastIndex(assertionSnapshot, nlnl)
	if contentSignatureSplit == -1 {
		return nil, fmt.Errorf("assertion content/signature separator not found")
	}
	content := assertionSnapshot[:contentSignatureSplit]
	signature := assertionSnapshot[contentSignatureSplit+2:]

	headersBodySplit := bytes.Index(content, nlnl)
	var body, head []byte
	if headersBodySplit == -1 {
		head = content
	} else {
		body = content[headersBodySplit+2:]
		if len(body) == 0 {
			body = nil
		}
		head = content[:headersBodySplit]
	}

	headers, err := parseHeaders(head)
	if err != nil {
		return nil, fmt.Errorf("parsing assertion headers: %v", err)
	}

	if len(signature) == 0 {
		return nil, fmt.Errorf("empty assertion signature")
	}

	return Assemble(headers, body, content, signature)
}

func checkRevision(headers map[string]string) (int, error) {
	revision, err := checkInteger(headers, "revision", 0)
	if err != nil {
		return -1, err
	}
	if revision < 0 {
		return -1, fmt.Errorf("revision should be positive: %v", revision)
	}
	return revision, nil
}

// Assemble assembles an assertion from its components.
func Assemble(headers map[string]string, body, content, signature []byte) (Assertion, error) {
	length, err := checkInteger(headers, "body-length", 0)
	if err != nil {
		return nil, fmt.Errorf("assertion: %v", err)
	}
	if length != len(body) {
		return nil, fmt.Errorf("assertion body length and declared body-length don't match: %v != %v", len(body), length)
	}

	if _, err := checkMandatory(headers, "authority-id"); err != nil {
		return nil, fmt.Errorf("assertion: %v", err)
	}

	typ, err := checkMandatory(headers, "type")
	if err != nil {
		return nil, fmt.Errorf("assertion: %v", err)
	}
	assertType := AssertionType(typ)
	reg, err := checkAssertType(assertType)
	if err != nil {
		return nil, err
	}

	revision, err := checkRevision(headers)
	if err != nil {
		return nil, fmt.Errorf("assertion: %v", err)
	}

	assert, err := reg.assembler(assertionBase{
		headers:   headers,
		body:      body,
		revision:  revision,
		content:   content,
		signature: signature,
	})
	if err != nil {
		return nil, fmt.Errorf("assertion %v: %v", assertType, err)
	}
	return assert, nil
}

func writeHeader(buf *bytes.Buffer, headers map[string]string, name string) {
	buf.WriteByte('\n')
	buf.WriteString(name)
	buf.WriteString(": ")
	buf.WriteString(headers[name])
}

func assembleAndSign(assertType AssertionType, headers map[string]string, body []byte, privKey PrivateKey) (Assertion, error) {
	finalHeaders := make(map[string]string, len(headers))
	for name, value := range headers {
		finalHeaders[name] = value
	}
	bodyLength := len(body)
	finalBody := make([]byte, bodyLength)
	copy(finalBody, body)
	finalHeaders["type"] = string(assertType)
	finalHeaders["body-length"] = strconv.Itoa(bodyLength)

	if _, err := checkMandatory(finalHeaders, "authority-id"); err != nil {
		return nil, err
	}

	reg, err := checkAssertType(assertType)
	if err != nil {
		return nil, err
	}

	revision, err := checkRevision(finalHeaders)
	if err != nil {
		return nil, err
	}

	buf := bytes.NewBufferString("type: ")
	buf.WriteString(string(assertType))

	writeHeader(buf, finalHeaders, "authority-id")
	if revision > 0 {
		writeHeader(buf, finalHeaders, "revision")
	} else {
		delete(finalHeaders, "revision")
	}
	written := map[string]bool{
		"type":         true,
		"authority-id": true,
		"revision":     true,
		"body-length":  true,
	}
	for _, primKey := range reg.primaryKey {
		if _, err := checkMandatory(finalHeaders, primKey); err != nil {
			return nil, err
		}
		writeHeader(buf, finalHeaders, primKey)
		written[primKey] = true
	}

	// emit other headers in lexicographic order
	otherKeys := make([]string, 0, len(finalHeaders))
	for name := range finalHeaders {
		if !written[name] {
			otherKeys = append(otherKeys, name)
		}
	}
	sort.Strings(otherKeys)
	for _, k := range otherKeys {
		writeHeader(buf, finalHeaders, k)
	}

	// body-length and body
	if bodyLength > 0 {
		writeHeader(buf, finalHeaders, "body-length")
	} else {
		delete(finalHeaders, "body-length")
	}
	if bodyLength > 0 {
		buf.Grow(bodyLength + 2)
		buf.Write(nlnl)
		buf.Write(finalBody)
	} else {
		finalBody = nil
	}
	content := buf.Bytes()

	signature, err := signContent(content, privKey)
	if err != nil {
		return nil, fmt.Errorf("failed to sign assertion: %v", err)
	}
	// be 'cat' friendly, add a ignored newline to the signature which is the last part of the encoded assertion
	signature = append(signature, '\n')

	assert, err := reg.assembler(assertionBase{
		headers:   finalHeaders,
		body:      finalBody,
		revision:  revision,
		content:   content,
		signature: signature,
	})
	if err != nil {
		return nil, fmt.Errorf("cannot assemble assertion %v: %v", assertType, err)
	}
	return assert, nil
}

// registry for assertion types describing how to assemble them etc...

type assertionTypeRegistration struct {
	assembler  func(assert assertionBase) (Assertion, error)
	primaryKey []string
}

var typeRegistry = make(map[AssertionType]*assertionTypeRegistration)

func primaryKey(t AssertionType) []string {
	reg, ok := typeRegistry[t]
	if !ok {
		panic("unknown assertion type used as value: " + string(t))
	}
	return reg.primaryKey
}

// Encode serializes an assertion.
func Encode(assert Assertion) []byte {
	content, signature := assert.Signature()
	needed := len(content) + 2 + len(signature)
	buf := bytes.NewBuffer(make([]byte, 0, needed))
	buf.Write(content)
	buf.Write(nlnl)
	buf.Write(signature)
	return buf.Bytes()
}
