// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2024 Canonical Ltd
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
	"encoding/json"
	"fmt"
	"regexp"
	"time"

	"github.com/snapcore/snapd/aspects"
)

// AspectBundle holds an aspect-bundle assertion, which is a definition by an
// account of access aspects ("views") and a storage schema for a set of
// related configuration options under the purview of the account.
type AspectBundle struct {
	assertionBase

	bundle    *aspects.Bundle
	timestamp time.Time
}

// AccountID returns the identifier of the account that signed this assertion.
func (ab *AspectBundle) AccountID() string {
	return ab.HeaderString("account-id")
}

// Name returns the name for the bundle.
func (ab *AspectBundle) Name() string {
	return ab.HeaderString("name")
}

// Bundle returns a aspects.Bundle implementing the aspect bundle configuration
// handling.
func (ab *AspectBundle) Bundle() *aspects.Bundle {
	return ab.bundle
}

var (
	validAspectBundleName = regexp.MustCompile("^[a-z0-9](?:-?[a-z0-9])*$")
)

func assembleAspectBundle(assert assertionBase) (Assertion, error) {
	authorityID := assert.AuthorityID()
	accountID := assert.HeaderString("account-id")
	if accountID != authorityID {
		return nil, fmt.Errorf("authority-id and account-id must match, aspect-bundle assertions are expected to be signed by the issuer account: %q != %q", authorityID, accountID)
	}

	name, err := checkStringMatches(assert.headers, "name", validAspectBundleName)
	if err != nil {
		return nil, err
	}

	aspectsMap, err := checkMap(assert.headers, "aspects")
	if err != nil {
		return nil, err
	}
	if aspectsMap == nil {
		return nil, fmt.Errorf(`"aspects" stanza is mandatory`)
	}

	if _, err := checkOptionalString(assert.headers, "summary"); err != nil {
		return nil, err
	}

	var bodyMap map[string]json.RawMessage
	if err := json.Unmarshal(assert.body, &bodyMap); err != nil {
		return nil, err
	}

	schemaRaw, ok := bodyMap["storage"]
	if !ok {
		return nil, fmt.Errorf(`body must contain a "storage" stanza`)
	}

	schema, err := aspects.ParseSchema(schemaRaw)
	if err != nil {
		return nil, fmt.Errorf(`invalid schema: %w`, err)
	}

	bundle, err := aspects.NewBundle(accountID, name, aspectsMap, schema)
	if err != nil {
		return nil, err
	}

	timestamp, err := checkRFC3339Date(assert.headers, "timestamp")
	if err != nil {
		return nil, err
	}

	return &AspectBundle{
		assertionBase: assert,
		bundle:        bundle,
		timestamp:     timestamp,
	}, nil
}
