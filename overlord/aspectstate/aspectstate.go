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

// Set finds the aspect identified by the account, bundleName and aspect and sets
// the specified field to the supplied value.
func Set(st *state.State, account, bundleName, aspect, field string, value interface{}) error {
	databag, err := getDatabag(st, account, bundleName)
	if err != nil {
		if !errors.Is(err, state.ErrNoState) {
			return err
		}

		databag = aspects.NewJSONDataBag()
	}

	accPatterns := aspecttest.MockWifiSetupAspect()
	aspectBundle, err := aspects.NewAspectBundle(bundleName, accPatterns, aspects.NewJSONSchema())
	if err != nil {
		return err
	}

	asp := aspectBundle.Aspect(aspect)
	if asp == nil {
		return &aspects.AspectNotFoundError{Account: account, BundleName: bundleName, Aspect: aspect}
	}

	if err := asp.Set(databag, field, value); err != nil {
		return err
	}

	if err := updateDatabags(st, account, bundleName, databag); err != nil {
		return err
	}

	return nil
}

// Get finds the aspect identified by the account, bundleName and aspect and
// returns the specified field's value through the "value" output parameter.
func Get(st *state.State, account, bundleName, aspect, field string, value interface{}) error {
	databag, err := getDatabag(st, account, bundleName)
	if err != nil {
		if errors.Is(err, state.ErrNoState) {
			return &aspects.AspectNotFoundError{Account: account, BundleName: bundleName, Aspect: aspect}
		}

		return err
	}

	accPatterns := aspecttest.MockWifiSetupAspect()
	aspectBundle, err := aspects.NewAspectBundle(bundleName, accPatterns, aspects.NewJSONSchema())
	if err != nil {
		return err
	}

	asp := aspectBundle.Aspect(aspect)
	if asp == nil {
		return &aspects.AspectNotFoundError{Account: account, BundleName: bundleName, Aspect: aspect}
	}

	if err := asp.Get(databag, field, value); err != nil {
		return err
	}

	return nil
}

func updateDatabags(st *state.State, account, bundleName string, databag aspects.JSONDataBag) error {
	var databags map[string]map[string]aspects.JSONDataBag
	if err := st.Get("aspect-databags", &databags); err != nil {
		if !errors.Is(err, state.ErrNoState) {
			return err
		}

		databags = map[string]map[string]aspects.JSONDataBag{
			account: {bundleName: aspects.NewJSONDataBag()},
		}
	}

	databags[account][bundleName] = databag
	st.Set("aspect-databags", databags)
	return nil
}

func getDatabag(st *state.State, account, bundleName string) (aspects.JSONDataBag, error) {
	var databags map[string]map[string]aspects.JSONDataBag
	if err := st.Get("aspect-databags", &databags); err != nil {
		return nil, err
	}
	return databags[account][bundleName], nil
}
