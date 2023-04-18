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

func Set(st *state.State, account, directory, aspect, field, value string) error {
	st.Lock()
	defer st.Unlock()

	databag, err := getDatabag(st, account, directory, aspect)
	if err != nil {
		if !errors.Is(err, state.ErrNoState) && !errors.Is(err, &aspects.NotFoundError{}) {
			return err
		}

		databag = aspects.NewJSONDataBag()
	}

	accPatterns, err := aspecttest.MockAspect(account, directory)
	if err != nil {
		return err
	}

	aspectDir, err := aspects.NewAspectDirectory(directory, accPatterns, databag, aspects.NewJSONSchema())
	if err != nil {
		return err
	}

	asp := aspectDir.Aspect(aspect)
	if asp == nil {
		return notFound("aspect %q was not found", aspect)
	}

	// delete the entry
	var val interface{}
	if value != "" {
		val = value
	}

	if err := asp.Set(field, val); err != nil {
		return err
	}

	if err := updateDatabags(st, account, directory, aspect, databag); err != nil {
		return err
	}

	return nil
}

func Get(st *state.State, account, directory, aspect, field string) (string, error) {
	st.Lock()
	defer st.Unlock()

	databag, err := getDatabag(st, account, directory, aspect)
	if err != nil {
		if errors.Is(err, state.ErrNoState) {
			return "", notFound("aspect %s/%s/%s was not found", account, directory, aspect)
		}

		return "", err
	}

	accPatterns, err := aspecttest.MockAspect(account, directory)
	if err != nil {
		return "", err
	}

	aspectDir, err := aspects.NewAspectDirectory(directory, accPatterns, databag, aspects.NewJSONSchema())
	if err != nil {
		return "", err
	}

	asp := aspectDir.Aspect(aspect)
	if asp == nil {
		return "", notFound("aspect %s/%s/%s was not found", account, directory, aspect)
	}

	var value string
	if err := asp.Get(field, &value); err != nil {
		return "", err
	}

	return value, nil
}

func updateDatabags(st *state.State, account, directory, aspect string, databag aspects.JSONDataBag) error {
	var databags map[string]map[string]map[string]aspects.JSONDataBag
	if err := st.Get("aspect-databags", &databags); err != nil {
		if !errors.Is(err, state.ErrNoState) {
			return err
		}

		databags = map[string]map[string]map[string]aspects.JSONDataBag{
			account: {directory: {}},
		}
	}

	databags[account][directory][aspect] = databag
	st.Set("aspect-databags", databags)
	return nil
}

func getDatabag(st *state.State, account, directory, aspect string) (aspects.JSONDataBag, error) {
	var databags map[string]map[string]map[string]aspects.JSONDataBag
	if err := st.Get("aspect-databags", &databags); err != nil {
		return nil, err
	}
	return databags[account][directory][aspect], nil
}

func notFound(msg string, v ...interface{}) *aspects.NotFoundError {
	return &aspects.NotFoundError{Message: fmt.Sprintf(msg, v...)}
}
