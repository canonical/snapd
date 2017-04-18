// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2016 Canonical Ltd
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

// Package tool offers tooling to sign assertions.
package tool

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"gopkg.in/yaml.v2"

	"github.com/snapcore/snapd/asserts"
)

// The supported media types for the input of assertion signing.
const (
	JSONInput = "application/json"
	YAMLInput = "application/x-yaml"
)

// SignRequest lets specify the complete input for signing an assertion.
type SignRequest struct {
	// the key to use can be speficied either passing the text of
	// an account-key assertion
	AccountKey []byte
	// or passing the key id
	KeyID string
	// and an optional account-id of the signer (if left out headers value are consulted)
	AuthorityID string

	// the assertion type (as a string)
	AssertionType string
	// specify the media type of the input
	StatementMediaType string
	// Statement is used as input to construct the assertion
	// it's a mapping encoded as either JSON or YAML (specified in StatementMediaType)
	// either of just the header fields of the assertion
	// or containting exactly two entries
	// "headers": mapping with the header fields
	// "content-body": used as the content body of the assertion
	Statement []byte

	// revision of the new assertion
	Revision int
}

func parseStatement(req *SignRequest, dest interface{}) error {
	switch req.StatementMediaType {
	case YAMLInput:
		err := yaml.Unmarshal(req.Statement, dest)
		if err != nil {
			return fmt.Errorf("cannot parse the assertion input as YAML: %v", err)
		}
	case JSONInput:
		dec := json.NewDecoder(bytes.NewBuffer(req.Statement))
		// we want control over supporting only integers
		dec.UseNumber()
		err := dec.Decode(dest)
		if err != nil {
			return fmt.Errorf("cannot parse the assertion input as JSON: %v", err)
		}
	default:
		return fmt.Errorf("unsupported media type for assertion input: %q", req.StatementMediaType)
	}
	return nil
}

type nestedStatement struct {
	Headers     map[string]interface{} `yaml:"headers" json:"headers"`
	ContentBody string                 `yaml:"content-body" json:"content-body"`
}

// Sign produces the text of a signed assertion as specified by req.
func Sign(req *SignRequest, keypairMgr asserts.KeypairManager) ([]byte, error) {
	typ := asserts.Type(req.AssertionType)
	if typ == nil {
		return nil, fmt.Errorf("invalid assertion type: %q", req.AssertionType)
	}
	if req.Revision < 0 {
		return nil, fmt.Errorf("assertion revision cannot be negative")
	}
	if req.AccountKey == nil && req.KeyID == "" {
		return nil, fmt.Errorf("both account-key and key id were not specified")
	}

	var nestedStatement nestedStatement
	err := parseStatement(req, &nestedStatement)
	if err != nil {
		return nil, err
	}
	if nestedStatement.Headers == nil {
		// flat headers, reparse
		err := parseStatement(req, &nestedStatement.Headers)
		if err != nil {
			return nil, err
		}
	}

	headers, err := stringify(nestedStatement.Headers)
	if err != nil {
		return nil, err
	}
	body := []byte(nestedStatement.ContentBody)

	keyID := req.KeyID
	authorityID := req.AuthorityID

	if req.AccountKey != nil {
		// use the account-key as a handle to get the information about
		// signer and key id
		a, err := asserts.Decode(req.AccountKey)
		if err != nil {
			return nil, fmt.Errorf("cannot parse handle account-key: %v", err)
		}
		accKey, ok := a.(*asserts.AccountKey)
		if !ok {
			return nil, fmt.Errorf("cannot use handle account-key, not actually an account-key, got: %T", a)
		}
		keyID = accKey.PublicKeyID()
		authorityID = accKey.AccountID()

		// extra sanity checks about fingerprint
		pk, err := keypairMgr.Get(authorityID, keyID)
		if err != nil {
			return nil, err
		}
		expFpr := accKey.PublicKeyFingerprint()
		gotFpr := pk.PublicKey().Fingerprint()
		if gotFpr != expFpr {
			return nil, fmt.Errorf("cannot use found private key, fingerprint does not match accout-key, expected %q, got: %s", expFpr, gotFpr)
		}
	}

	if authorityID != "" {
		headers["authority-id"] = authorityID
	}

	if req.Revision != 0 {
		headers["revision"] = strconv.Itoa(req.Revision)
	}

	adb, err := asserts.OpenDatabase(&asserts.DatabaseConfig{
		KeypairManager: keypairMgr,
	})
	if err != nil {
		return nil, err
	}

	a, err := adb.Sign(typ, headers, body, keyID)
	if err != nil {
		return nil, err
	}

	return asserts.Encode(a), nil
}

// let the invoker use a limited amount of structured input without having
// to convert everything obvious to strings upfront on their side,
// we convert integers, bool (to yes|no), list of strings (to comma separated) and nil (to empty)

func stringify(m map[string]interface{}) (map[string]string, error) {
	res := make(map[string]string, len(m))
	for k, w := range m {
		var s string
		switch v := w.(type) {
		case nil:
			s = ""
		case bool:
			if v {
				s = "yes"
			} else {
				s = "no"
			}
		case string:
			s = v
		case json.Number:
			_, err := v.Int64()
			if err != nil {
				return nil, fmt.Errorf("cannot turn header field %q number value into an integer (other number types are not supported): %v", k, w)
			}
			s = v.String()
		case int:
			s = strconv.Itoa(v)
		case []interface{}:
			elems := make([]string, len(v))
			for i, wel := range v {
				el, ok := wel.(string)
				if !ok {
					return nil, fmt.Errorf("cannot turn header field %q list value into string, has non-string element with type %T: %v", k, wel, wel)
				}
				elems[i] = el
			}
			// TODO: split over many lines if too long
			s = strings.Join(elems, ",")
		default:
			return nil, fmt.Errorf("cannot turn header field %q value with type %T into string: %v", k, w, w)
		}
		res[k] = s
	}
	return res, nil
}
