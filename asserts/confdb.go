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
	"errors"
	"fmt"
	"time"

	"github.com/snapcore/snapd/confdb"
)

// ConfdbSchema holds a confdb-schema assertion, which is a definition by an
// account of access views and a storage schema for a set of related
// configuration options under the purview of the account.
type ConfdbSchema struct {
	assertionBase

	schema    *confdb.Schema
	timestamp time.Time
}

// AccountID returns the identifier of the account that signed this assertion.
func (ar *ConfdbSchema) AccountID() string {
	return ar.HeaderString("account-id")
}

// Name returns the name for the confdb.
func (ar *ConfdbSchema) Name() string {
	return ar.HeaderString("name")
}

// Schema returns a confdb.Schema assembled from the assertion that can
// be used to access confdb views.
func (ar *ConfdbSchema) Schema() *confdb.Schema {
	return ar.schema
}

func assembleConfdbSchema(assert assertionBase) (Assertion, error) {
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

	schema, err := confdb.ParseStorageSchema(schemaRaw)
	if err != nil {
		return nil, fmt.Errorf(`invalid schema: %w`, err)
	}

	confdbSchema, err := confdb.NewSchema(accountID, name, viewsMap, schema)
	if err != nil {
		return nil, err
	}

	timestamp, err := checkRFC3339Date(assert.headers, "timestamp")
	if err != nil {
		return nil, err
	}

	return &ConfdbSchema{
		assertionBase: assert,
		schema:        confdbSchema,
		timestamp:     timestamp,
	}, nil
}

// ConfdbControl holds a confdb-control assertion, which holds lists of
// views delegated by the device to operators.
type ConfdbControl struct {
	assertionBase

	control *confdb.Control
}

// expected interfaces are implemented
var (
	_ customSigner = (*ConfdbControl)(nil)
)

// signKey returns the public key of the device that signed this assertion.
func (cc *ConfdbControl) signKey(db RODatabase) (PublicKey, error) {
	a, err := db.Find(SerialType, map[string]string{
		"brand-id": cc.BrandID(),
		"model":    cc.Model(),
		"serial":   cc.Serial(),
	})
	if err != nil {
		return nil, fmt.Errorf("cannot find matching device serial assertion: %w", err)
	}

	serial := a.(*Serial)
	key := serial.DeviceKey()
	if key.ID() != cc.SignKeyID() {
		return nil, errors.New("confdb-control's signing key doesn't match the device key")
	}

	return key, nil
}

// Prerequisites returns references to this confdb-control's prerequisite assertions.
func (cc *ConfdbControl) Prerequisites() []*Ref {
	return []*Ref{
		{Type: SerialType, PrimaryKey: []string{cc.BrandID(), cc.Model(), cc.Serial()}},
	}
}

// BrandID returns the brand identifier of the device.
func (cc *ConfdbControl) BrandID() string {
	return cc.HeaderString("brand-id")
}

// Model returns the model name identifier of the device.
func (cc *ConfdbControl) Model() string {
	return cc.HeaderString("model")
}

// Serial returns the serial identifier of the device.
// Together with brand-id and model, they form the device's unique identifier.
func (cc *ConfdbControl) Serial() string {
	return cc.HeaderString("serial")
}

// Control returns the confdb.Control reflecting the assertion.
func (cc *ConfdbControl) Control() confdb.Control {
	return cc.control.Clone()
}

// assembleConfdbControl creates a new confdb-control assertion after validating
// all required fields and constraints.
func assembleConfdbControl(assert assertionBase) (Assertion, error) {
	_, err := checkStringMatches(assert.headers, "brand-id", validAccountID)
	if err != nil {
		return nil, err
	}

	if _, err := checkModel(assert.headers); err != nil {
		return nil, err
	}

	serial := assert.HeaderString("serial")
	if !validSerialStrict.MatchString(serial) {
		return nil, fmt.Errorf("invalid serial: %s", serial)
	}

	groups, err := checkList(assert.headers, "groups")
	if err != nil {
		return nil, err
	}

	cc, err := parseConfdbControlGroups(groups)
	if err != nil {
		return nil, err
	}

	return &ConfdbControl{
		assertionBase: assert,
		control:       cc,
	}, nil
}

func parseConfdbControlGroups(rawGroups []any) (*confdb.Control, error) {
	cc := &confdb.Control{}
	for i, rawGroup := range rawGroups {
		errPrefix := fmt.Sprintf("cannot parse group at position %d", i+1)

		group, ok := rawGroup.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("%s: must be a map", errPrefix)
		}

		auth, err := checkStringListInMap(group, "authentications", `"authentications" field`, nil)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", errPrefix, err)
		}
		if auth == nil {
			return nil, fmt.Errorf(`%s: "authentications" must be provided`, errPrefix)
		}

		views, err := checkStringListInMap(group, "views", `"views" field`, nil)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", errPrefix, err)
		}
		if views == nil {
			return nil, fmt.Errorf(`%s: "views" must be provided`, errPrefix)
		}

		operatorIDs, err := checkStringListInMap(group, "operators", `"operators" field`, nil)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", errPrefix, err)
		}
		if operatorIDs == nil {
			return nil, fmt.Errorf(`%s: "operators" must be provided`, errPrefix)
		}

		for _, operatorID := range operatorIDs {
			if err := cc.Delegate(operatorID, views, auth); err != nil {
				return nil, fmt.Errorf("%s: %w", errPrefix, err)
			}
		}
	}

	return cc, nil
}
