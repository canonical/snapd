// -*- Mode: Go; indent-tabs-mode: t -*-
/*
 * Copyright (C) 2023 Canonical Ltd
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

package aspectstate

import (
	"errors"

	"github.com/snapcore/snapd/aspects"
	"github.com/snapcore/snapd/overlord/aspectstate/aspecttest"
	"github.com/snapcore/snapd/overlord/state"
)

// SetAspect finds the aspect identified by the account, bundleName and aspect
// and sets the specified field to the supplied value in the provided matching databag.
func SetAspect(databag aspects.DataBag, account, bundleName, aspect, field string, value interface{}) error {
	accPatterns := aspecttest.MockWifiSetupAspect()
	schema := aspects.NewJSONSchema()

	aspectBundle, err := aspects.NewBundle(account, bundleName, accPatterns, schema)
	if err != nil {
		return err
	}

	asp := aspectBundle.Aspect(aspect)
	if asp == nil {
		return &aspects.NotFoundError{
			Account:    account,
			BundleName: bundleName,
			Aspect:     aspect,
			Operation:  "set",
			Request:    field,
			Cause:      "aspect not found",
		}
	}

	return asp.Set(databag, field, value)
}

// GetAspect finds the aspect identified by the account, bundleName and aspect
// and returns the specified field value from the provided matching databag
// through the value output parameter.
func GetAspect(databag aspects.DataBag, account, bundleName, aspect, field string) (interface{}, error) {
	accPatterns := aspecttest.MockWifiSetupAspect()
	schema := aspects.NewJSONSchema()

	aspectBundle, err := aspects.NewBundle(account, bundleName, accPatterns, schema)
	if err != nil {
		return nil, err
	}

	asp := aspectBundle.Aspect(aspect)
	if asp == nil {
		return nil, &aspects.NotFoundError{
			Account:    account,
			BundleName: bundleName,
			Aspect:     aspect,
			Operation:  "get",
			Request:    field,
			Cause:      "aspect not found",
		}
	}

	return asp.Get(databag, field)
}

// NewTransaction returns a transaction configured to read and write databags
// from state as needed.
func NewTransaction(st *state.State, account, bundleName string) (*aspects.Transaction, error) {
	schema := aspects.NewJSONSchema()
	getter := bagGetter(st, account, bundleName)
	setter := func(bag aspects.JSONDataBag) error {
		return updateDatabags(st, account, bundleName, bag)
	}

	tx, err := aspects.NewTransaction(getter, setter, schema)
	if err != nil {
		return nil, err
	}

	return tx, nil
}

func bagGetter(st *state.State, account, bundleName string) aspects.DatabagRead {
	return func() (aspects.JSONDataBag, error) {
		databag, err := getDatabag(st, account, bundleName)
		if err != nil {
			if !errors.Is(err, state.ErrNoState) {
				return nil, err
			}

			databag = aspects.NewJSONDataBag()
		}
		return databag, nil
	}
}

func getDatabag(st *state.State, account, bundleName string) (aspects.JSONDataBag, error) {
	var databags map[string]map[string]aspects.JSONDataBag
	if err := st.Get("aspect-databags", &databags); err != nil {
		return nil, err
	}

	if databags[account] == nil || databags[account][bundleName] == nil {
		return nil, state.ErrNoState
	}
	return databags[account][bundleName], nil
}

func updateDatabags(st *state.State, account, bundleName string, databag aspects.JSONDataBag) error {
	var databags map[string]map[string]aspects.JSONDataBag
	err := st.Get("aspect-databags", &databags)
	if err != nil && !errors.Is(err, state.ErrNoState) {
		return err
	} else if errors.Is(err, &state.NoStateError{}) || databags[account] == nil || databags[account][bundleName] == nil {
		databags = map[string]map[string]aspects.JSONDataBag{
			account: {bundleName: aspects.NewJSONDataBag()},
		}
	}

	databags[account][bundleName] = databag
	st.Set("aspect-databags", databags)
	return nil
}
