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
	"time"

	"github.com/snapcore/snapd/confdb"
)

// Confdb holds a confdb assertion, which is a definition by an account of
// access views and a storage schema for a set of related configuration options
// under the purview of the account.
type Confdb struct {
	assertionBase

	confdb    *confdb.Confdb
	timestamp time.Time
}

// AccountID returns the identifier of the account that signed this assertion.
func (ar *Confdb) AccountID() string {
	return ar.HeaderString("account-id")
}

// Name returns the name for the confdb.
func (ar *Confdb) Name() string {
	return ar.HeaderString("name")
}

// Confdb returns a Confdb assembled from the assertion that can be used
// to access confdb views.
func (ar *Confdb) Confdb() *confdb.Confdb {
	return ar.confdb
}

func assembleConfdb(assert assertionBase) (Assertion, error) {
	authorityID := assert.AuthorityID()
	accountID := assert.HeaderString("account-id")
	if accountID != authorityID {
		return nil, fmt.Errorf("authority-id and account-id must match, confdb assertions are expected to be signed by the issuer account: %q != %q", authorityID, accountID)
	}

	name, err := checkStringMatches(assert.headers, "name", confdb.ValidConfdbName)
	if err != nil {
		return nil, err
	}

	viewsMap, err := checkMap(assert.headers, "views")
	if err != nil {
		return nil, err
	}
	if viewsMap == nil {
		return nil, fmt.Errorf(`"views" stanza is mandatory`)
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

	schema, err := confdb.ParseSchema(schemaRaw)
	if err != nil {
		return nil, fmt.Errorf(`invalid schema: %w`, err)
	}

	confdb, err := confdb.New(accountID, name, viewsMap, schema)
	if err != nil {
		return nil, err
	}

	timestamp, err := checkRFC3339Date(assert.headers, "timestamp")
	if err != nil {
		return nil, err
	}

	return &Confdb{
		assertionBase: assert,
		confdb:        confdb,
		timestamp:     timestamp,
	}, nil
}
