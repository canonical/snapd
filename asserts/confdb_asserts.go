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
	"fmt"

	"github.com/snapcore/snapd/confdb"
)

// ConfdbControl holds a confdb-control assertion, which holds lists of
// views delegated by the device to an operator.
type ConfdbControl struct {
	assertionBase

	// the key is the operator ID
	operators map[string]*confdb.Operator
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

// assembleConfdbControl creates a new confdb-control assertion after validating
// all required fields and constraints.
func assembleConfdbControl(assert assertionBase) (Assertion, error) {
	// Validate headers
	_, err := checkStringMatches(assert.headers, "brand-id", validAccountID)
	if err != nil {
		return nil, err
	}

	_, err = checkModel(assert.headers)
	if err != nil {
		return nil, err
	}

	groups, err := checkList(assert.headers, "groups")
	if err != nil {
		return nil, err
	}
	if groups == nil {
		return nil, fmt.Errorf(`"groups" stanza is mandatory`)
	}

	// Create the confdb-control assertion
	cc := &ConfdbControl{
		assertionBase: assert,
		operators:     make(map[string]*confdb.Operator),
	}

	// Parse the groups in the assertion
	err = parseConfdbControlGroups(cc, groups)
	if err != nil {
		return nil, err
	}

	return cc, nil
}

func parseConfdbControlGroups(cc *ConfdbControl, rawGroups []interface{}) error {
	for i, rawGroup := range rawGroups {
		errPrefix := fmt.Sprintf("group at position %d", i+1)

		group, ok := rawGroup.(map[string]interface{})
		if !ok {
			return fmt.Errorf("%s: must be a map", errPrefix)
		}

		operatorID, err := checkNotEmptyStringWhat(group, "operator-id", "field")
		if err != nil {
			return fmt.Errorf("%s: %w", errPrefix, err)
		}

		// Currently, operatorIDs must be snap store account IDs
		if !IsValidAccountID(operatorID) {
			return fmt.Errorf(`%s: invalid "operator-id" %s`, errPrefix, operatorID)
		}

		operator, ok := cc.operators[operatorID]
		if !ok {
			operator = &confdb.Operator{ID: operatorID}
			cc.operators[operatorID] = operator
		}

		auth, err := checkStringListInMap(group, "authentication", "field", nil)
		if err != nil {
			return fmt.Errorf(`%s: "authentication" %w`, errPrefix, err)
		}
		if auth == nil {
			return fmt.Errorf(`%s: "authentication" must be provided`, errPrefix)
		}

		views, err := checkStringListInMap(group, "views", "field", nil)
		if err != nil {
			return fmt.Errorf(`%s: "views" %w`, errPrefix, err)
		}
		if views == nil {
			return fmt.Errorf(`%s: "views" must be provided`, errPrefix)
		}

		err = operator.AddGroup(views, auth)
		if err != nil {
			return fmt.Errorf(`%s: %w`, errPrefix, err)
		}
	}

	return nil
}
