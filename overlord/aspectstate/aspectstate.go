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
	"fmt"

	"github.com/snapcore/snapd/aspects"
	"github.com/snapcore/snapd/overlord/aspectstate/aspecttest"
	"github.com/snapcore/snapd/overlord/state"
)

func Set(st *state.State, account, bundleName, aspect, field string, value interface{}) error {
	st.Lock()
	defer st.Unlock()

	databag, err := getDatabag(st, account, bundleName)
	if err != nil {
		if !errors.Is(err, state.ErrNoState) && !errors.Is(err, &aspects.NotFoundError{}) {
			return err
		}

		databag = aspects.NewJSONDataBag()
	}

	accPatterns, err := aspecttest.MockAspect(account, bundleName)
	if err != nil {
		return err
	}

	aspectBundle, err := aspects.NewAspectBundle(bundleName, accPatterns, aspects.NewJSONSchema())
	if err != nil {
		return err
	}

	asp := aspectBundle.Aspect(aspect)
	if asp == nil {
		return notFound("aspect %s/%s/%s was not found", account, bundleName, aspect)
	}

	if err := asp.Set(databag, field, value); err != nil {
		return err
	}

	if err := updateDatabags(st, account, bundleName, databag); err != nil {
		return err
	}

	return nil
}

func Get(st *state.State, account, bundleName, aspect, field string, value interface{}) error {
	st.Lock()
	defer st.Unlock()

	databag, err := getDatabag(st, account, bundleName)
	if err != nil {
		if errors.Is(err, state.ErrNoState) {
			return notFound("aspect %s/%s/%s was not found", account, bundleName, aspect)
		}

		return err
	}

	accPatterns, err := aspecttest.MockAspect(account, bundleName)
	if err != nil {
		return err
	}

	aspectBundle, err := aspects.NewAspectBundle(bundleName, accPatterns, aspects.NewJSONSchema())
	if err != nil {
		return err
	}

	asp := aspectBundle.Aspect(aspect)
	if asp == nil {
		return notFound("aspect %s/%s/%s was not found", account, bundleName, aspect)
	}

	if err := asp.Get(databag, field, &value); err != nil {
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

func notFound(msg string, v ...interface{}) *aspects.NotFoundError {
	return &aspects.NotFoundError{Message: fmt.Sprintf(msg, v...)}
}
