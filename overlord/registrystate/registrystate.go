// -*- Mode: Go; indent-tabs-mode: t -*-
/*
 * Copyright (C) 2023-2024 Canonical Ltd
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

package registrystate

import (
	"errors"

	"github.com/snapcore/snapd/overlord/assertstate"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/registry"
)

// SetViaView finds the view identified by the account, registry and view names
// and sets the request fields to their respective values.
func SetViaView(st *state.State, account, registryName, viewName string, requests map[string]interface{}) error {
	registryAssert, err := assertstate.Registry(st, account, registryName)
	if err != nil {
		return err
	}
	reg := registryAssert.Registry()

	view := reg.View(viewName)
	if view == nil {
		keys := make([]string, 0, len(requests))
		for k := range requests {
			keys = append(keys, k)
		}

		return &registry.NotFoundError{
			Account:      account,
			RegistryName: registryName,
			View:         viewName,
			Operation:    "set",
			Requests:     keys,
			Cause:        "not found",
		}
	}

	tx, err := newTransaction(st, reg)
	if err != nil {
		return err
	}

	for field, value := range requests {
		var err error
		if value == nil {
			err = view.Unset(tx, field)
		} else {
			err = view.Set(tx, field, value)
		}

		if err != nil {
			return err
		}
	}

	return tx.Commit()
}

// GetViaView finds the view identified by the account, registry and view names
// and uses it to get the values for the specified fields. The results are
// returned in a map of fields to their values, unless there are no fields in
// which case all views are returned.
func GetViaView(st *state.State, account, registryName, viewName string, fields []string) (interface{}, error) {
	registryAssert, err := assertstate.Registry(st, account, registryName)
	if err != nil {
		return nil, err
	}
	reg := registryAssert.Registry()

	view := reg.View(viewName)
	if view == nil {
		return nil, &registry.NotFoundError{
			Account:      account,
			RegistryName: registryName,
			View:         viewName,
			Operation:    "get",
			Requests:     fields,
			Cause:        "not found",
		}
	}

	tx, err := newTransaction(st, reg)
	if err != nil {
		return nil, err
	}

	if len(fields) == 0 {
		val, err := view.Get(tx, "")
		if err != nil {
			return nil, err
		}

		return val, nil
	}

	results := make(map[string]interface{}, len(fields))
	for _, field := range fields {
		value, err := view.Get(tx, field)
		if err != nil {
			if errors.Is(err, &registry.NotFoundError{}) && len(fields) > 1 {
				// keep looking; return partial result if only some fields are found
				continue
			}

			return nil, err
		}

		results[field] = value
	}

	if len(results) == 0 {
		return nil, &registry.NotFoundError{
			Account:      account,
			RegistryName: registryName,
			View:         viewName,
			Operation:    "get",
			Requests:     fields,
			Cause:        "matching rules don't map to any values",
		}
	}

	return results, nil
}

// newTransaction returns a transaction configured to read and write databags
// from state as needed.
func newTransaction(st *state.State, reg *registry.Registry) (*registry.Transaction, error) {
	getter := bagGetter(st, reg)
	setter := func(bag registry.JSONDataBag) error {
		return updateDatabags(st, bag, reg)
	}

	tx, err := registry.NewTransaction(getter, setter, reg.Schema)
	if err != nil {
		return nil, err
	}

	return tx, nil
}

func bagGetter(st *state.State, reg *registry.Registry) registry.DatabagRead {
	return func() (registry.JSONDataBag, error) {
		databag, err := getDatabag(st, reg.Account, reg.Name)
		if err != nil {
			if !errors.Is(err, state.ErrNoState) {
				return nil, err
			}

			databag = registry.NewJSONDataBag()
		}
		return databag, nil
	}
}

func getDatabag(st *state.State, account, registryName string) (registry.JSONDataBag, error) {
	var databags map[string]map[string]registry.JSONDataBag
	if err := st.Get("registry-databags", &databags); err != nil {
		return nil, err
	}

	if databags[account] == nil || databags[account][registryName] == nil {
		return nil, state.ErrNoState
	}
	return databags[account][registryName], nil
}

func updateDatabags(st *state.State, databag registry.JSONDataBag, reg *registry.Registry) error {
	account := reg.Account
	registryName := reg.Name

	var databags map[string]map[string]registry.JSONDataBag
	err := st.Get("registry-databags", &databags)
	if err != nil && !errors.Is(err, state.ErrNoState) {
		return err
	} else if errors.Is(err, &state.NoStateError{}) || databags[account] == nil || databags[account][registryName] == nil {
		databags = map[string]map[string]registry.JSONDataBag{
			account: {registryName: registry.NewJSONDataBag()},
		}
	}

	databags[account][registryName] = databag
	st.Set("registry-databags", databags)
	return nil
}
