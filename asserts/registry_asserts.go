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

	registryControl *registry.RegistryControl
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

// OperatorID returns the identifier of the operator the device
// has delegated registry control to.
func (rgCtrl *RegistryControl) OperatorID() string {
	return rgCtrl.HeaderString("operator-id")
}

// RegistryControl returns a RegistryControl assembled from the assertion that
// can be used to access the delegation information.
func (rgCtrl *RegistryControl) RegistryControl() *registry.RegistryControl {
	return rgCtrl.registryControl
}

// assembleRegistryControl assembles the registry-control assertion.
// TODO: Confirm that the brand-id, model, & serial match the device's serial assertion
func assembleRegistryControl(assert assertionBase) (Assertion, error) {
	_, err := checkStringMatches(assert.headers, "brand-id", validAccountID)
	if err != nil {
		return nil, err
	}

	_, err = checkModel(assert.headers)
	if err != nil {
		return nil, err
	}

	views, err := checkList(assert.headers, "views")
	if err != nil {
		return nil, err
	}
	if views == nil {
		return nil, fmt.Errorf(`"views" stanza is mandatory`)
	}

	operatorID := assert.HeaderString("operator-id")
	rgCtrl, err := registry.NewRegistryControl(operatorID, views)
	if err != nil {
		return nil, err
	}

	return &RegistryControl{
		assertionBase:   assert,
		registryControl: rgCtrl,
	}, nil
}
