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
	"github.com/snapcore/snapd/overlord/assertstate"
	"github.com/snapcore/snapd/overlord/state"
)

// SetAspect finds the aspect identified by the account, bundleName and aspect
// and sets the request fields to their respective values.
func SetAspect(st *state.State, account, bundleName, aspect string, requests map[string]interface{}) error {
	bundleAssert, err := assertstate.AspectBundle(st, account, bundleName)
	if err != nil {
		return err
	}
	bundle := bundleAssert.Bundle()

	asp := bundle.Aspect(aspect)
	if asp == nil {
		keys := make([]string, 0, len(requests))
		for k := range requests {
			keys = append(keys, k)
		}

		return &aspects.NotFoundError{
			Account:    account,
			BundleName: bundleName,
			Aspect:     aspect,
			Operation:  "set",
			Requests:   keys,
			Cause:      "aspect not found",
		}
	}

	tx, err := newTransaction(st, bundle)
	if err != nil {
		return err
	}

	for field, value := range requests {
		if err := asp.Set(tx, field, value); err != nil {
			return err
		}
	}

	return tx.Commit()
}

// GetAspect finds the aspect identified by the account, bundleName and aspect
// and uses it to get the values for the specified fields. The results are
// returned in a map of fields to their values, unless there are no fields in
// which case the entire aspect is just returned as-is.
func GetAspect(st *state.State, account, bundleName, aspect string, fields []string) (interface{}, error) {
	bundleAssert, err := assertstate.AspectBundle(st, account, bundleName)
	if err != nil {
		return nil, err
	}
	bundle := bundleAssert.Bundle()

	asp := bundle.Aspect(aspect)
	if asp == nil {
		return nil, &aspects.NotFoundError{
			Account:    account,
			BundleName: bundleName,
			Aspect:     aspect,
			Operation:  "get",
			Requests:   fields,
			Cause:      "aspect not found",
		}
	}

	tx, err := newTransaction(st, bundle)
	if err != nil {
		return nil, err
	}

	if len(fields) == 0 {
		val, err := asp.Get(tx, "")
		if err != nil {
			return nil, err
		}

		return val, nil
	}

	results := make(map[string]interface{}, len(fields))
	for _, field := range fields {
		value, err := asp.Get(tx, field)
		if err != nil {
			if errors.Is(err, &aspects.NotFoundError{}) && len(fields) > 1 {
				// keep looking; return partial result if only some fields are found
				continue
			}

			return nil, err
		}

		results[field] = value
	}

	if len(results) == 0 {
		return nil, &aspects.NotFoundError{
			Account:    account,
			BundleName: bundleName,
			Aspect:     aspect,
			Operation:  "get",
			Requests:   fields,
			Cause:      "matching rules don't map to any values",
		}
	}

	return results, nil
}

// newTransaction returns a transaction configured to read and write databags
// from state as needed.
func newTransaction(st *state.State, bundle *aspects.Bundle) (*aspects.Transaction, error) {
	getter := bagGetter(st, bundle)
	setter := func(bag aspects.JSONDataBag) error {
		return updateDatabags(st, bag, bundle)
	}

	tx, err := aspects.NewTransaction(getter, setter, bundle.Schema)
	if err != nil {
		return nil, err
	}

	return tx, nil
}

func bagGetter(st *state.State, bundle *aspects.Bundle) aspects.DatabagRead {
	return func() (aspects.JSONDataBag, error) {
		databag, err := getDatabag(st, bundle.Account, bundle.Name)
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

func updateDatabags(st *state.State, databag aspects.JSONDataBag, bundle *aspects.Bundle) error {
	account := bundle.Account
	bundleName := bundle.Name

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
