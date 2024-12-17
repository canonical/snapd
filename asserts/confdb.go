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
	"sort"
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

// ConfdbControl holds a confdb-control assertion, which holds lists of
// views delegated by the device to operators.
type ConfdbControl struct {
	assertionBase

	// the key is the operator ID
	operators map[string]*confdb.Operator
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

// NewConfdbControl returns an empty confdb-control assertion.
func NewConfdbControl(serial *Serial) *ConfdbControl {
	return &ConfdbControl{
		assertionBase: assertionBase{
			headers: map[string]interface{}{
				"type":     "confdb-control",
				"brand-id": serial.BrandID(),
				"model":    serial.Model(),
				"serial":   serial.Serial(),
				"groups":   []interface{}{},
			},
		},
		operators: map[string]*confdb.Operator{},
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

// IsDelegated checks if the view is delegated to the operator with the given auth.
func (cc *ConfdbControl) IsDelegated(operatorID, view string, auth []string) (bool, error) {
	operator, ok := cc.operators[operatorID]
	if !ok {
		return false, nil // nothing is delegated to this operator
	}

	return operator.IsDelegated(view, auth)
}

// Delegate delegates the given views with the provided auth to the operator.
func (cc *ConfdbControl) Delegate(operatorID string, views, auth []string) error {
	if len(operatorID) == 0 {
		return errors.New("operator-id is required")
	}

	operator, ok := cc.operators[operatorID]
	if !ok {
		operator = &confdb.Operator{ID: operatorID}
	}

	err := operator.Delegate(views, auth)
	if err != nil {
		return err
	}

	cc.operators[operatorID] = operator
	return nil
}

// Revoke withdraws access to the views that have been delegated with the provided auth.
func (cc *ConfdbControl) Revoke(operatorID string, views, auth []string) error {
	operator, ok := cc.operators[operatorID]
	if !ok {
		return nil // nothing is delegated to this operator
	}

	if len(views) == 0 && len(auth) == 0 {
		delete(cc.operators, operatorID) // completely revoke access from this operator
		return nil
	}

	return operator.Revoke(views, auth)
}

// Groups returns the groups in the raw assertion's format.
func (cc *ConfdbControl) Groups() []interface{} {
	ids := make([]string, 0, len(cc.operators))
	for id := range cc.operators {
		ids = append(ids, id)
	}
	sort.Strings(ids)

	var groups []interface{}
	for _, id := range ids { // sorted by operator
		op := cc.operators[id]
		for _, group := range op.Groups {
			auth, views := []interface{}{}, []interface{}{}
			for _, a := range group.Authentication {
				auth = append(auth, string(a))
			}

			for _, v := range group.Views {
				views = append(views, v.String())
			}

			groups = append(groups, map[string]interface{}{
				"operator-id": op.ID, "authentication": auth, "views": views,
			})
		}
	}
	return groups
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

	groups, err := checkList(assert.headers, "groups")
	if err != nil {
		return nil, err
	}
	if groups == nil {
		return nil, errors.New(`"groups" stanza is mandatory`)
	}

	operators, err := parseConfdbControlGroups(groups)
	if err != nil {
		return nil, err
	}

	cc := &ConfdbControl{
		assertionBase: assert,
		operators:     operators,
	}
	return cc, nil
}

func parseConfdbControlGroups(rawGroups []interface{}) (map[string]*confdb.Operator, error) {
	operators := map[string]*confdb.Operator{}
	for i, rawGroup := range rawGroups {
		errPrefix := fmt.Sprintf("cannot parse group at position %d", i+1)

		group, ok := rawGroup.(map[string]interface{})
		if !ok {
			return nil, fmt.Errorf("%s: must be a map", errPrefix)
		}

		operatorID, err := checkNotEmptyStringWhat(group, "operator-id", "field")
		if err != nil {
			return nil, fmt.Errorf("%s: %w", errPrefix, err)
		}

		// Currently, operatorIDs must be snap store account IDs
		if !IsValidAccountID(operatorID) {
			return nil, fmt.Errorf(`%s: invalid "operator-id" %s`, errPrefix, operatorID)
		}

		operator, ok := operators[operatorID]
		if !ok {
			operator = &confdb.Operator{ID: operatorID}
			operators[operatorID] = operator
		}

		auth, err := checkStringListInMap(group, "authentication", "field", nil)
		if err != nil {
			return nil, fmt.Errorf(`%s: "authentication" %w`, errPrefix, err)
		}
		if auth == nil {
			return nil, fmt.Errorf(`%s: "authentication" must be provided`, errPrefix)
		}

		views, err := checkStringListInMap(group, "views", "field", nil)
		if err != nil {
			return nil, fmt.Errorf(`%s: "views" %w`, errPrefix, err)
		}
		if views == nil {
			return nil, fmt.Errorf(`%s: "views" must be provided`, errPrefix)
		}

		if err := operator.Delegate(views, auth); err != nil {
			return nil, fmt.Errorf(`%s: %w`, errPrefix, err)
		}
	}

	return operators, nil
}
