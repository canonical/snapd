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

package asserts

import (
	"bytes"
	"fmt"
	"strconv"
	"time"
)

var builtinAssertions []Assertion

// builtinSignature is the special signature marker used for builtin assertions.
var builtinSignature = []byte("$builtin")

func isBuiltinSignature(sig []byte) bool {
	return bytes.Equal(bytes.TrimRight(sig, "\n"), builtinSignature)
}

// Builtin returns a copy of the list of builtin assertions which is assembled
// at init-time.
func Builtin() []Assertion {
	builtins := make([]Assertion, 0, len(builtinAssertions))
	for _, as := range builtinAssertions {
		builtins = append(builtins, as)
	}
	return builtins
}

type builtinCheckParams struct {
	// order determines the order in which headers are checked.
	order []string
	// expectedHeaders carries the expected values for non-optional header fields.
	expectedHeaders map[string]any
}

// assembleBuiltinAssertion creates a builtin assertion of the given type.
// The header bytes are expected to be YAML and are checked against the
// expected headers in checkParams. The assembled assertion carries a special
// "$builtin" signature marker and is not subject to normal trust verification.
func assembleBuiltinAssertion(assertType *AssertionType, headerBytes, body []byte, checkParams builtinCheckParams) (Assertion, error) {
	content := bytes.TrimSpace(headerBytes)
	h, err := parseHeaders(content)
	if err != nil {
		return nil, err
	}

	if len(checkParams.order) != len(checkParams.expectedHeaders) {
		return nil, fmt.Errorf("internal error: inconsistent length of order checking list (%d) and expected values map (%d)",
			len(checkParams.order),
			len(checkParams.expectedHeaders),
		)
	}

	for _, field := range checkParams.order {
		expected, ok := checkParams.expectedHeaders[field]
		if !ok {
			return nil, fmt.Errorf("the builtin %s %q header is missing an expected value", assertType.Name, field)
		}

		if h[field] != expected {
			return nil, fmt.Errorf("the builtin %s %q header is not set to expected value %q", assertType.Name, field, expected)
		}
	}

	if _, ok := h["sign-key-sha3-384"]; ok {
		return nil, fmt.Errorf(`cannot assemble builtin %s with "sign-key-sha3-384": cannot be signed`, assertType.Name)
	}

	revision, err := checkRevision(h)
	if err != nil {
		return nil, fmt.Errorf("cannot assemble the builtin %s: %v", assertType.Name, err)
	}

	// inject headers required for encode/decode roundtrip, if any are missing
	if _, ok := h["timestamp"]; !ok {
		ts := time.Now().UTC().Format(time.RFC3339)
		h["timestamp"] = ts
		content = append(content, []byte("\ntimestamp: "+ts)...)
	}

	if _, ok := h["body-length"]; !ok && len(body) > 0 {
		h["body-length"] = strconv.Itoa(len(body))
		content = append(content, []byte(fmt.Sprintf("\nbody-length: %d", len(body)))...)
		content = append(content, nlnl...)
		content = append(content, body...)
	}

	a, err := assertType.assembler(assertionBase{
		headers:   h,
		body:      body,
		revision:  revision,
		content:   content,
		signature: builtinSignature,
	})
	if err != nil {
		return nil, fmt.Errorf("cannot assemble the builtin %s: %v", assertType.Name, err)
	}

	return a, nil
}

// RegisterBuiltinConfdbSchema registers a builtin confdb-schema assertion.
func RegisterBuiltinConfdbSchema(headerBytes, body []byte) error {
	a, err := assembleBuiltinAssertion(ConfdbSchemaType, headerBytes, body, builtinCheckParams{
		order: []string{"type", "account-id", "authority-id"},
		expectedHeaders: map[string]any{
			"type":         "confdb-schema",
			"account-id":   "system",
			"authority-id": "canonical",
		},
	})
	if err != nil {
		return err
	}
	builtinAssertions = append(builtinAssertions, a)
	return nil
}
