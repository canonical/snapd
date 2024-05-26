// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2016-2023 Canonical Ltd
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

// Package signtool offers tooling to sign assertions.
package signtool

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/ddkwork/golibrary/mylog"
	"github.com/snapcore/snapd/asserts"
)

var Stdout = os.Stdout

// Options specifies the complete input for signing an assertion.
type Options struct {
	// KeyID specifies the key id of the key to use
	KeyID string

	// AccountKey optionally holds the account-key for the key to use,
	// used for cross-checking
	AccountKey *asserts.AccountKey

	// Statement is used as input to construct the assertion
	// it's a mapping encoded as JSON
	// of the header fields of the assertion
	// plus an optional pseudo-header "body" to specify
	// the body of the assertion
	Statement []byte

	// Complement specifies complementary headers to what is in
	// Statement, for use by tools that fill-in/compute some of
	// the headers. Headers appearing both in Statement and
	// Complement are an error, except for "type" that needs
	// instead to match if present. Pseudo-header "body" can also
	// be specified here.
	Complement map[string]interface{}
}

// Sign produces the text of a signed assertion as specified by opts.
func Sign(opts *Options, keypairMgr asserts.KeypairManager) ([]byte, error) {
	var headers map[string]interface{}
	mylog.Check(json.Unmarshal(opts.Statement, &headers))

	for name, value := range opts.Complement {
		if v, ok := headers[name]; ok {
			if name == "type" {
				if v != value {
					return nil, fmt.Errorf("repeated assertion type does not match")
				}
			} else {
				return nil, fmt.Errorf("complementary header %q clashes with assertion input", name)
			}
		}
		headers[name] = value
	}

	typCand, ok := headers["type"]
	if !ok {
		return nil, fmt.Errorf("missing assertion type header")
	}
	typStr, ok := typCand.(string)
	if !ok {
		return nil, fmt.Errorf("assertion type must be a string, not: %v", typCand)
	}
	typ := asserts.Type(typStr)
	if typ == nil {
		return nil, fmt.Errorf("invalid assertion type: %v", headers["type"])
	}

	var body []byte
	if bodyCand, ok := headers["body"]; ok {
		bodyStr, ok := bodyCand.(string)
		if !ok {
			return nil, fmt.Errorf("body if specified must be a string")
		}
		body = []byte(bodyStr)
		delete(headers, "body")
	}

	adb := mylog.Check2(asserts.OpenDatabase(&asserts.DatabaseConfig{
		KeypairManager: keypairMgr,
	}))

	if opts.AccountKey != nil {
		// cross-check with the actual account-key if provided
		accKey := opts.AccountKey
		if accKey.PublicKeyID() != opts.KeyID {
			return nil, fmt.Errorf("internal error: key id does not match the signing account-key")
		}
		if accKey.AccountID() != headers["authority-id"] {
			return nil, fmt.Errorf("authority-id does not match the account-id of the signing account-key")
		}
		if accKey.ConstraintsPrecheck(typ, headers) != nil {
			return nil, fmt.Errorf("the assertion headers do not match the constraints of the signing account-key")
		}
	}

	a := mylog.Check2(adb.Sign(typ, headers, body, opts.KeyID))

	return asserts.Encode(a), nil
}
