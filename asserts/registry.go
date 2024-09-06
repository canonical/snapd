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
	"sort"
	"time"

	"github.com/snapcore/snapd/registry"
)

// Registry holds a registry assertion, which is a definition by an account of
// access views and a storage schema for a set of related configuration options
// under the purview of the account.
type Registry struct {
	assertionBase

	registry  *registry.Registry
	timestamp time.Time
}

// AccountID returns the identifier of the account that signed this assertion.
func (ar *Registry) AccountID() string {
	return ar.HeaderString("account-id")
}

// Name returns the name for the registry.
func (ar *Registry) Name() string {
	return ar.HeaderString("name")
}

// Registry returns a Registry assembled from the assertion that can be used
// to access registry views.
func (ar *Registry) Registry() *registry.Registry {
	return ar.registry
}

func assembleRegistry(assert assertionBase) (Assertion, error) {
	authorityID := assert.AuthorityID()
	accountID := assert.HeaderString("account-id")
	if accountID != authorityID {
		return nil, fmt.Errorf("authority-id and account-id must match, registry assertions are expected to be signed by the issuer account: %q != %q", authorityID, accountID)
	}

	name, err := checkStringMatches(assert.headers, "name", registry.ValidRegistryName)
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

	schema, err := registry.ParseSchema(schemaRaw)
	if err != nil {
		return nil, fmt.Errorf(`invalid schema: %w`, err)
	}

	registry, err := registry.New(accountID, name, viewsMap, schema)
	if err != nil {
		return nil, err
	}

	timestamp, err := checkRFC3339Date(assert.headers, "timestamp")
	if err != nil {
		return nil, err
	}

	return &Registry{
		assertionBase: assert,
		registry:      registry,
		timestamp:     timestamp,
	}, nil
}

// RegistryControl holds a registry-control assertion, which holds a list of
// registry views delegated by the device to an operator.
type RegistryControl struct {
	assertionBase

	// the key is the operator ID
	operators map[string]*registry.Operator
}

// BrandID returns the brand identifier of the device.
func (rgCtrl *RegistryControl) BrandID() string {
	return rgCtrl.HeaderString("brand-id")
}

// Model returns the model name identifier of the device.
func (rgCtrl *RegistryControl) Model() string {
	return rgCtrl.HeaderString("model")
}

// Serial returns the serial identifier of the device, together with
// brand id and model they form the unique identifier of the device.
func (rgCtrl *RegistryControl) Serial() string {
	return rgCtrl.HeaderString("serial")
}

// IsDelegated checks if <accountID>/<registry>/<view> is delegated to
// <operatorID> under the authentication method <auth>.
func (rgCtrl *RegistryControl) IsDelegated(operatorID, view, auth string) bool {
	operator, ok := rgCtrl.operators[operatorID]
	if !ok {
		// nothing is delegated to this operator
		return false
	}

	authMethod, err := registry.StringToAuthenticationMethod(auth)
	if err != nil {
		// unknown authentication method
		return false
	}

	return operator.IsDelegated(view, authMethod)
}

// Delegate delegates the given views under the provided authentication methods to the operator.
func (rgCtrl *RegistryControl) Delegate(operatorID string, views []string, authentication []string) error {
	operator, ok := rgCtrl.operators[operatorID]
	if !ok {
		operator = &registry.Operator{
			OperatorID: operatorID,
			Groups:     make([]*registry.Group, 0),
		}
	}

	authMethods, err := stringListToAuthenticationMethods(authentication)
	if err != nil {
		return err
	}

	err = operator.Delegate(views, authMethods)
	if err != nil {
		return err
	}

	rgCtrl.operators[operatorID] = operator
	return nil
}

// Revoke withdraws remote access to the views that have been delegated under
// the authentication methods.
func (rgCtrl *RegistryControl) Revoke(operatorID string, views []string, authentication []string) error {
	operator, ok := rgCtrl.operators[operatorID]
	if !ok {
		// nothing is delegated to this operator
		return nil
	}

	if len(views) == 0 && len(authentication) == 0 {
		// completely revoke access from this operator
		delete(rgCtrl.operators, operatorID)
		return nil
	}

	authMethods, err := stringListToAuthenticationMethods(authentication)
	if err != nil {
		return err
	}

	return operator.Revoke(views, authMethods)
}

// assembleRegistryControl assembles the registry-control assertion.
func assembleRegistryControl(assert assertionBase) (Assertion, error) {
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

	rgCtrl := &RegistryControl{
		assertionBase: assert,
		operators:     make(map[string]*registry.Operator),
	}

	err = parseRegistryControlGroups(rgCtrl, groups)
	if err != nil {
		return nil, err
	}

	return rgCtrl, nil
}

func parseRegistryControlGroups(rgCtrl *RegistryControl, rawGroups []interface{}) error {
	for i, rawGroup := range rawGroups {
		errPrefix := fmt.Sprintf("group at position %d", i+1)

		group, ok := rawGroup.(map[string]interface{})
		if !ok {
			return fmt.Errorf("%s: must be a map", errPrefix)
		}

		rawOperatorID, ok := group["operator-id"]
		if !ok {
			return fmt.Errorf(`%s: "operator-id" not provided`, errPrefix)
		}

		operatorID, ok := rawOperatorID.(string)
		if !ok || len(operatorID) == 0 {
			return fmt.Errorf(`%s: "operator-id" must be a non-empty string`, errPrefix)
		}

		// while in the future we'll accommodate other RBAC systems,
		// for now operatorIDs must be snap store account IDs
		if !IsValidAccountID(operatorID) {
			return fmt.Errorf(`%s: invalid "operator-id" %s`, errPrefix, operatorID)
		}

		authentication, err := checkStringListInMap(group, "authentication", "field", nil)
		if err != nil {
			return fmt.Errorf(`%s: "authentication" %w`, errPrefix, err)
		}
		if authentication == nil {
			return fmt.Errorf(`%s: "authentication" must be provided`, errPrefix)
		}

		views, err := checkStringListInMap(group, "views", "field", nil)
		if err != nil {
			return fmt.Errorf(`%s: "views" %w`, errPrefix, err)
		}
		if views == nil {
			return fmt.Errorf(`%s: "views" must be provided`, errPrefix)
		}

		err = rgCtrl.Delegate(operatorID, views, authentication)
		if err != nil {
			return fmt.Errorf("%s: %w", errPrefix, err)
		}
	}

	return nil
}

func stringListToAuthenticationMethods(authentication []string) ([]registry.AuthenticationMethod, error) {
	authMethods := make([]registry.AuthenticationMethod, 0, len(authentication))
	for _, auth := range authentication {
		authMethod, err := registry.StringToAuthenticationMethod(auth)
		if err != nil {
			return nil, err
		}

		authMethods = append(authMethods, authMethod)
	}
	return authMethods, nil
}

// PrintGroups returns a string representation of the groups in the assertion
// it's only used for testing
func (rgCtrl RegistryControl) PrintGroups() string {
	operatorIDs := make([]string, 0, len(rgCtrl.operators))
	for key := range rgCtrl.operators {
		operatorIDs = append(operatorIDs, key)
	}
	sort.Strings(operatorIDs)

	result := "groups:\n"
	for _, operatorID := range operatorIDs {
		operator := rgCtrl.operators[operatorID]
		for _, group := range operator.Groups {
			result += fmt.Sprintf("  -\n    operator-id: %s\n", operatorID)

			result += "    authentication:\n"
			for _, auth := range group.Authentication {
				result += fmt.Sprintf("      - %s\n", auth.String())
			}

			result += "    views:\n"
			for _, view := range group.Views {
				result += fmt.Sprintf("      - %s\n", view)
			}
		}
	}

	return result
}
