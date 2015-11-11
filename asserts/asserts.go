// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2014-2015 Canonical Ltd
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
	"encoding/base64"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"unicode/utf8"
)

// AssertionType labels assertions of a given type
type AssertionType string

// Understood assertions
const (
// IdentityType AssertionType = ...
// AccountKeyType AssertionType = "account-key"
// ...
)

// Assertion signature format types
const (
	OpenPGPSig = "openpgp"
)

// Assertion represents an assertion through its general elements.
type Assertion interface {
	// the type of this assertion
	Type() AssertionType
	// the revision of this assertion
	Revision() int
	// the authority that signed this assertion
	AuthorityID() string

	// Header retrieves the header with name
	Header(name string) string

	// the body of this assertion
	Body() []byte

	// Signature returns the signed content and its signature already split and decoded
	Signature() (content []byte, sigtype string, signature []byte)
}

// AssertionBase is the concrete base to hold representation data for actual assertions.
type AssertionBase struct {
	headers map[string]string
	body    []byte
	// parsed revision
	revision int
	// preserved content
	content []byte
	// signature format/type
	sigtype string
	// decoded signature
	sigpacket []byte
}

// Type returns the assertion type.
func (ab *AssertionBase) Type() AssertionType {
	return AssertionType(ab.headers["type"])
}

// Revision returns the assertion revision.
func (ab *AssertionBase) Revision() int {
	return ab.revision
}

// AuthorityID returns the authority-id a.k.a the signer id of the assertion.
func (ab *AssertionBase) AuthorityID() string {
	return ab.headers["authority-id"]
}

// Header returns the value of an header by name.
func (ab *AssertionBase) Header(name string) string {
	return ab.headers[name]
}

// Body returns the body of the assertion.
func (ab *AssertionBase) Body() []byte {
	return ab.body
}

// Signature returns the signed content and its signature already split and decoded.
func (ab *AssertionBase) Signature() (content []byte, sigtype string, signature []byte) {
	return ab.content, ab.sigtype, ab.sigpacket
}

// sanity check
var _ Assertion = (*AssertionBase)(nil)

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
			return nil, fmt.Errorf("invalid header name: %v", name)
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
//    SIGNATURE is composed like:
//      SIG-TYPE " " BASE64-SIG-PACKET
//
//    SIG-TYPE is the type/format expected for the base64
//    encoded signature value, for now only "openpgp" is supported
//
// An header line looks like:
//
//   NAME ": " VALUE
//
// for sanity NAME is expected to match headerNameSanity regexp.
//
// The following headers are mandatory:
//
//   type
//   authority-id (the signer id)
//   revision (a positive int)
//   body-length (int expected to be equal to the length of BODY)
//
func Decode(serializedAssertion []byte) (Assertion, error) {
	contentSignatureSplit := bytes.LastIndex(serializedAssertion, nlnl)
	if contentSignatureSplit == -1 {
		return nil, fmt.Errorf("assertion content/signature separator not found")
	}
	content := serializedAssertion[:contentSignatureSplit]
	signature := serializedAssertion[contentSignatureSplit+2:]

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

	sigtypeSigpacketSplit := bytes.IndexByte(signature, ' ')
	if sigtypeSigpacketSplit == -1 {
		return nil, fmt.Errorf("could not split the assertion signature into type and base64 packet")
	}

	sigtype := string(signature[:sigtypeSigpacketSplit])
	sigpacket := signature[sigtypeSigpacketSplit+1:]
	if len(sigpacket) == 0 {
		return nil, fmt.Errorf("empty assertion signature packet")
	}
	decodedSigpacket := make([]byte, base64.StdEncoding.DecodedLen(len(sigpacket)))
	n, err := base64.StdEncoding.Decode(decodedSigpacket[:], sigpacket)
	if err != nil {
		return nil, fmt.Errorf("could not base64 decode the assertion signature packet")
	}
	sigpacket = decodedSigpacket[:n]

	return buildAssertion(headers, body, content, sigtype, sigpacket)
}

func buildAssertion(headers map[string]string, body, content []byte, sigtype string, sigpacket []byte) (Assertion, error) {

	checkInteger := func(name string) (int, error) {
		valueStr, ok := headers[name]
		if !ok {
			return -1, fmt.Errorf("assertion %v header is mandatory", name)
		}
		value, err := strconv.Atoi(valueStr)
		if err != nil {
			return -1, fmt.Errorf("assertion %v is not an integer: %v", name, valueStr)
		}
		return value, nil
	}

	length, err := checkInteger("body-length")
	if err != nil {
		return nil, err
	}
	if length != len(body) {
		return nil, fmt.Errorf("assertion body length and declared body-length don't match: %v != %v", len(body), length)
	}

	checkMandatory := func(name string) (string, error) {
		value, ok := headers[name]
		if !ok {
			return "", fmt.Errorf("assertion %v header is mandatory", name)
		}
		if len(value) == 0 {
			return "", fmt.Errorf("assertion %v should not be empty", name)
		}
		return value, nil
	}

	if _, err := checkMandatory("authority-id"); err != nil {
		return nil, err
	}

	// for now only openpgp is valid/expected
	if sigtype != OpenPGPSig {
		return nil, fmt.Errorf("unsupported assertion signature type: %v", sigtype)
	}

	assertType, err := checkMandatory("type")
	if err != nil {
		return nil, err
	}
	reg := typeRegistry[AssertionType(assertType)]
	if reg == nil {
		return nil, fmt.Errorf("cannot build assertion of unknown type: %v", assertType)
	}

	revision, err := checkInteger("revision")
	if err != nil {
		return nil, err
	}
	if revision < 0 {
		return nil, fmt.Errorf("assertion revision should be positive: %v", revision)
	}

	return reg.builder(AssertionBase{headers, body, revision, content, sigtype, sigpacket}), nil
}

// registry for assertion types describing how to build them etc...

type assertionTypeRegistration struct {
	builder func(assert AssertionBase) Assertion
}

var typeRegistry = make(map[AssertionType]*assertionTypeRegistration)
