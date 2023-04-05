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

	if err := aspecttest.MaybeMockAspect(st); err != nil {
		return err
	}

	var asps map[string]map[string]*aspects.Directory
	if err := st.Get("aspects", &asps); err != nil {
		if errors.Is(err, state.ErrNoState) {
			return notFound("no aspects were found")
		}

		return err
	}

	aspectDir, ok := asps[account][directory]
	if !ok {
		return notFound("%s's aspect directory %q was not found", account, directory)
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

	st.Set("aspects", asps)
	return nil
}

func Get(st *state.State, account, directory, aspect, field string) (string, error) {
	st.Lock()
	defer st.Unlock()

	if err := aspecttest.MaybeMockAspect(st); err != nil {
		return "", err
	}

	var asps map[string]map[string]*aspects.Directory
	if err := st.Get("aspects", &asps); err != nil {
		if errors.Is(err, state.ErrNoState) {
			return "", notFound("no aspects were found")
		}

		return "", err
	}

	aspectDir, ok := asps[account][directory]
	if !ok {
		return "", notFound("aspect %s/%s/%s was not found", account, directory, aspect)
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

func notFound(msg string, v ...interface{}) *aspects.NotFoundError {
	return &aspects.NotFoundError{Message: fmt.Sprintf(msg, v...)}
}
