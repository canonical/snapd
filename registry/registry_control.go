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

package registry

import (
	"errors"
	"fmt"
	"regexp"
	"strings"
)

var (
	validAccountID = regexp.MustCompile("^(?:[a-z0-9A-Z]{32}|[-a-z0-9]{2,28})$")
)

// RegistryControl holds a list of views delegated to an operator.
type RegistryControl struct {
	OperatorID string
	Registries map[string]*delegatedRegistry // key is <account-id>/<registry>
}

type delegatedRegistry struct {
	AccountID string
	Name      string
	Views     map[string]*delegatedView // key is the view's name
}

type delegatedView struct {
	Name string
}

// NewRegistryControl assembles a new RegistryControl with the specified views delegated
// to the provided operatorID.
func NewRegistryControl(operatorID string, views []interface{}) (*RegistryControl, error) {
	// while in the future we'll accommodate other RBAC systems,
	// for now operatorIDs must be snap store account IDs
	if !validAccountID.MatchString(operatorID) {
		return nil, fmt.Errorf("invalid Operator ID %s", operatorID)
	}

	if len(views) == 0 {
		return nil, errors.New("cannot define registry-control: no views provided")
	}

	delegated := make(map[string]*delegatedRegistry)
	registryControl := &RegistryControl{
		OperatorID: operatorID,
		Registries: delegated,
	}

	for i, view := range views {
		viewMap, ok := view.(map[string]interface{})
		if !ok || len(viewMap) == 0 {
			return nil, fmt.Errorf("view at position %d: must be a non-empty map", i+1)
		}

		name, ok := viewMap["name"]
		if !ok {
			return nil, fmt.Errorf(`view at position %d: "name" not provided`, i+1)
		}

		nameStr, ok := name.(string)
		if !ok || len(nameStr) == 0 {
			return nil, fmt.Errorf(`view at position %d: "name" must be a non-empty string`, i+1)
		}

		parts := strings.Split(nameStr, "/")
		if len(parts) != 3 {
			return nil, fmt.Errorf(`view at position %d: "name" must be in the format account/registry/view: %s`, i+1, nameStr)
		}

		accountID, registryName, viewName := parts[0], parts[1], parts[2]
		err := registryControl.Delegate(accountID, registryName, viewName)
		if err != nil {
			return nil, fmt.Errorf("view at position %d: %v", i+1, err)
		}
	}

	return registryControl, nil
}

// IsDelegated checks if RegistryControl delegates <account-id>/<registry>/<view>.
func (rgCtrl *RegistryControl) IsDelegated(accountID, registryName, view string) bool {
	key := fmt.Sprintf("%s/%s", accountID, registryName)

	registry, ok := rgCtrl.Registries[key]
	if !ok {
		return false
	}

	_, ok = registry.Views[view]
	return ok
}

// Delegate grants remote registry control to <account-id>/<registry>/<view>.
func (rgCtrl *RegistryControl) Delegate(accountID, registryName, view string) error {
	if !validAccountID.MatchString(accountID) {
		return fmt.Errorf("invalid Account ID %s", accountID)
	}

	if !ValidRegistryName.MatchString(registryName) {
		return fmt.Errorf("invalid registry name %s", registryName)
	}

	if !ValidViewName.MatchString(view) {
		return fmt.Errorf("invalid view name %s", view)
	}

	if rgCtrl.IsDelegated(accountID, registryName, view) {
		// already delegated, nothing to do
		return nil
	}

	key := fmt.Sprintf("%s/%s", accountID, registryName)
	registry, ok := rgCtrl.Registries[key]
	if !ok {
		registry = &delegatedRegistry{
			AccountID: accountID,
			Name:      registryName,
			Views:     make(map[string]*delegatedView),
		}

		rgCtrl.Registries[key] = registry
	}

	registry.Views[view] = &delegatedView{Name: view}

	return nil
}

// Revoke revokes remote registry control over <account-id>/<registry>/<view>.
func (rgCtrl *RegistryControl) Revoke(accountID, registryName, view string) {
	if !rgCtrl.IsDelegated(accountID, registryName, view) {
		// not delegated, nothing to do
		return
	}

	key := fmt.Sprintf("%s/%s", accountID, registryName)
	registry := rgCtrl.Registries[key]

	delete(registry.Views, view)
	// if the registry has no views delegated anymore, remove it
	if len(registry.Views) == 0 {
		delete(rgCtrl.Registries, key)
	}
}
