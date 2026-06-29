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

var (
	rawConfdbBuiltins = map[string]map[string][]byte{
		"validation-sets": {
			"headers": []byte(`type: confdb-schema
account-id: system
authority-id: canonical
name: validation-sets
summary: Manage the validation sets installed on a device
views:
  state:
    summary: Read the validation sets state
    rules:
      -
        request: {account}.{validation-set}
        storage: v1.{account}.{validation-set}
        access: read
        content:
          -
            storage: snaps[{n}]
            content:
              -
                storage: name
              -
                storage: id
              -
                storage: revision
              -
                storage: presence
              -
                storage: components
                content:
                  -
                    storage: {component}
                    content:
                      -
                        storage: presence
                      -
                        storage: revision
          -
            storage: mode
          -
            storage: status
          -
            storage: sequence
          -
            storage: revision
          -
            storage: pinned-sequence
  admin:
    summary: Control validation sets
    rules:
      -
        request: {account}.{set-name}
        storage: v1.{account}.{set-name}
        access: write
        content:
          -
            storage: pinned-sequence
          -
            storage: mode
      -
        request: {account}.{set-name}
        storage: v1.{account}.{set-name}
        access: read
        content:
          -
            storage: snaps[{n}]
            content:
              -
                storage: name
              -
                storage: id
              -
                storage: revision
              -
                storage: presence
          -
            storage: status
          -
            storage: sequence
          -
            storage: revision
          -
            storage: pinned-sequence
          -
            storage: mode
  pinning-admin:
    summary: Control pinning of validation sets
    rules:
      -
        request: {account}.{set-name}.pinned-sequence
        storage: v1.{account}.{set-name}.pinned-sequence
        access: read-write
`),
			// NOTE: JSON needs to be sorted, otherwise decoding validation would fail
			"body": []byte(`{
  "storage": {
    "aliases": {
      "account-id": {
        "pattern": "^(?:[a-z0-9A-Z]{32}|[-a-z0-9]{2,28})$",
        "type": "string"
      },
      "natural-number": {
        "min": 1,
        "type": "int"
      },
      "presence": {
        "choices": [
          "required",
          "optional",
          "invalid"
        ],
        "type": "string"
      },
      "set-name": {
        "pattern": "^[a-z0-9](?:-?[a-z0-9])*$",
        "type": "string"
      }
    },
    "schema": {
      "v1": {
        "keys": "${account-id}",
        "values": {
          "keys": "${set-name}",
          "values": {
            "required": [
              "mode"
            ],
            "schema": {
              "mode": {
                "choices": [
                  "monitor",
                  "enforce"
                ],
                "type": "string"
              },
              "pinned-sequence": "${natural-number}",
              "revision": "${natural-number}",
              "sequence": "${natural-number}",
              "snaps": {
                "type": "array",
                "unique": true,
                "values": {
                  "schema": {
                    "components": {
                      "values": {
                        "schema": {
                          "presence": "${presence}",
                          "revision": "${natural-number}"
                        }
                      }
                    },
                    "id": "string",
                    "name": "string",
                    "presence": "${presence}",
                    "revision": "${natural-number}"
                  }
                }
              },
              "status": {
                "choices": [
                  "valid",
                  "invalid"
                ],
                "type": "string"
              }
            }
          }
        }
      }
    }
  }
}`),
		},
	}

	confdbSchemaCheckOrder      = []string{"type", "account-id", "authority-id"}
	confdbSchemaExpectedHeaders = map[string]any{
		"type":         "confdb-schema",
		"account-id":   "system",
		"authority-id": "canonical",
	}
)

func init() {
	for name, builtin := range rawConfdbBuiltins {
		a, err := assembleBuiltinAssertion(ConfdbSchemaType, builtin["headers"], builtin["body"], builtinCheckParams{
			order:           confdbSchemaCheckOrder,
			expectedHeaders: confdbSchemaExpectedHeaders,
		})
		if err != nil {
			panic(fmt.Sprintf("cannot create builtin %q confdb-schema: %v", name, err))
		}
		builtinAssertions = append(builtinAssertions, a)
	}
}

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
	if accountID == "system" {
		if authorityID != "canonical" {
			return nil, fmt.Errorf(`"system" confdb-schemas must be signed by "canonical" got %q`, authorityID)
		}
	} else if accountID != authorityID {
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
