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
	"time"
)

var builtinAssertions []Assertion

func BuiltinAssertions() []Assertion {
	return builtinAssertions
}

type builtinCheckParams struct {
	// order determines the order in which headers are checked.
	order []string
	// expectedHeaders carries the expected values for non-optional header fields.
	expectedHeaders map[string]any
}

// assembleBuiltinAssertion creates a builtin assertion of the given type.
// The header bytes are expected to be YAML and are checked against the
// expected headers in checkParams. The account-id must be "system" and the
// authority-id must be "canonical". The assembled assertion carries a special
// "$builtin" signature marker and is not subject to normal trust verification.
func assembleBuiltinAssertion(assertType *AssertionType, headerBytes, body []byte, checkParams builtinCheckParams) (Assertion, error) {
	trimmed := bytes.TrimSpace(headerBytes)
	h, err := parseHeaders(trimmed)
	if err != nil {
		return nil, err
	}

	if acc := h["account-id"]; acc != "system" {
		return nil, fmt.Errorf(`the "account-id" for builtin %s must be set to "system"`, assertType.Name)
	}
	if auth := h["authority-id"]; auth != "canonical" {
		return nil, fmt.Errorf(`the "authority-id" for builtin %s must be set to "canonical"`, assertType.Name)
	}

	for _, field := range checkParams.order {
		expected := checkParams.expectedHeaders[field]
		if h[field] != expected {
			return nil, fmt.Errorf("the builtin %s %q header is not set to expected value %q", assertType.Name, field, expected)
		}
	}

	revision, err := checkRevision(h)
	if err != nil {
		return nil, fmt.Errorf("cannot assemble the builtin %s: %v", assertType.Name, err)
	}

	h["timestamp"] = time.Now().UTC().Format(time.RFC3339)
	a, err := assertType.assembler(assertionBase{
		headers:   h,
		body:      body,
		revision:  revision,
		content:   trimmed,
		signature: []byte("$builtin"),
	})
	if err != nil {
		return nil, fmt.Errorf("cannot assemble the builtin %s: %v", assertType.Name, err)
	}

	return a, nil
}
