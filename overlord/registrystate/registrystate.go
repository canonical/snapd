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
	"github.com/snapcore/snapd/overlord/hookstate"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/registry"
)

var assertstateRegistry = assertstate.Registry

// SetViaView finds the view identified by the account, registry and view names
// and sets the request fields to their respective values.
func SetViaView(st *state.State, account, registryName, viewName string, requests map[string]interface{}) error {
	registryAssert, err := assertstateRegistry(st, account, registryName)
	if err != nil {
		return err
	}
	reg := registryAssert.Registry()

	view := reg.View(viewName)
	if view == nil {
		var keys []string
		if len(requests) > 0 {
			keys = make([]string, 0, len(requests))
			for k := range requests {
				keys = append(keys, k)
			}
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

	tx, err := NewTransaction(st, account, registryName)
	if err != nil {
		return err
	}

	if err = SetViaViewInTx(tx, view, requests); err != nil {
		return err
	}

	return tx.Commit(st, reg.Schema)
}

// SetViaViewInTx uses the view to set the requests in the transaction's databag.
func SetViaViewInTx(tx *Transaction, view *registry.View, requests map[string]interface{}) error {
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

	return nil
}

// GetViaView finds the view identified by the account, registry and view names
// and uses it to get the values for the specified fields. The results are
// returned in a map of fields to their values, unless there are no fields in
// which case all views are returned.
func GetViaView(st *state.State, account, registryName, viewName string, fields []string) (interface{}, error) {
	registryAssert, err := assertstateRegistry(st, account, registryName)
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

	tx, err := NewTransaction(st, account, registryName)
	if err != nil {
		return nil, err
	}

	return GetViaViewInTx(tx, view, fields)
}

// GetViaViewInTx uses the view to get values for the fields from the databag
// in the transaction.
func GetViaViewInTx(tx *Transaction, view *registry.View, fields []string) (interface{}, error) {
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
			Account:      tx.RegistryAccount,
			RegistryName: tx.RegistryName,
			View:         view.Name,
			Operation:    "get",
			Requests:     fields,
			Cause:        "matching rules don't map to any values",
		}
	}

	return results, nil
}

var readDatabag = func(st *state.State, account, registryName string) (registry.JSONDataBag, error) {
	var databags map[string]map[string]registry.JSONDataBag
	if err := st.Get("registry-databags", &databags); err != nil {
		if errors.Is(err, &state.NoStateError{}) {
			return registry.NewJSONDataBag(), nil
		}
		return nil, err
	}

	if databags[account] == nil || databags[account][registryName] == nil {
		return registry.NewJSONDataBag(), nil
	}

	return databags[account][registryName], nil
}

var writeDatabag = func(st *state.State, databag registry.JSONDataBag, account, registryName string) error {
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

type cachedRegistryTx struct {
	account  string
	registry string
}

// RegistryTransaction returns the registry.Transaction cached in the context
// or creates one and caches it, if none existed. The context must be locked by
// the caller.
func RegistryTransaction(ctx *hookstate.Context, reg *registry.Registry) (*Transaction, error) {
	key := cachedRegistryTx{
		account:  reg.Account,
		registry: reg.Name,
	}
	tx, ok := ctx.Cached(key).(*Transaction)
	if ok {
		return tx, nil
	}

	tx, err := NewTransaction(ctx.State(), reg.Account, reg.Name)
	if err != nil {
		return nil, err
	}

	ctx.OnDone(func() error {
		return tx.Commit(ctx.State(), reg.Schema)
	})

	ctx.Cache(key, tx)
	return tx, nil
}
