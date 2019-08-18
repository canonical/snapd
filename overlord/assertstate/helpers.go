// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2016-2017 Canonical Ltd
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

package assertstate

import (
	"fmt"
	"strings"

	"github.com/snapcore/snapd/asserts"
	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/overlord/auth"
	"github.com/snapcore/snapd/overlord/snapstate"
	"github.com/snapcore/snapd/overlord/state"
)

// TODO: snapstate also has this, move to auth, or change a bit the approach now that we have DeviceAndAuthContext in the store?
func userFromUserID(st *state.State, userID int) (*auth.UserState, error) {
	if userID == 0 {
		return nil, nil
	}
	return auth.User(st, userID)
}

type accumFetcher struct {
	asserts.Fetcher
	fetched []asserts.Assertion
}

// newAccumFetcher creates an accumFetcher used to retrieve assertions and later commit them to the system database in one go.
func newAccumFetcher(db *asserts.Database, retrieve func(*asserts.Ref) (asserts.Assertion, error)) *accumFetcher {
	f := &accumFetcher{}

	save := func(a asserts.Assertion) error {
		f.fetched = append(f.fetched, a)
		return nil
	}

	f.Fetcher = asserts.NewFetcher(db, retrieve, save)

	return f
}

type commitError struct {
	errs []error
}

func (e *commitError) Error() string {
	l := []string{""}
	for _, e := range e.errs {
		l = append(l, e.Error())
	}
	return fmt.Sprintf("cannot add some assertions to the system database:%s", strings.Join(l, "\n - "))
}

// commitTo does a best effort of adding all the fetched assertions to the system database.
func commitTo(db *asserts.Database, assertions []asserts.Assertion) error {
	var errs []error
	for _, a := range assertions {
		err := db.Add(a)
		if asserts.IsUnaccceptedUpdate(err) {
			if _, ok := err.(*asserts.UnsupportedFormatError); ok {
				// we kept the old one, but log the issue
				logger.Noticef("Cannot update assertion %v: %v", a.Ref(), err)
			}
			// be idempotent
			// system db has already the same or newer
			continue
		}
		if err != nil {
			errs = append(errs, err)
		}
	}
	if len(errs) != 0 {
		return &commitError{errs: errs}
	}
	return nil
}

func doFetch(s *state.State, userID int, deviceCtx snapstate.DeviceContext, fetching func(asserts.Fetcher) error) error {
	// TODO: once we have a bulk assertion retrieval endpoint this approach will change

	user, err := userFromUserID(s, userID)
	if err != nil {
		return err
	}

	sto := snapstate.Store(s, deviceCtx)

	retrieve := func(ref *asserts.Ref) (asserts.Assertion, error) {
		// TODO: ignore errors if already in db?
		return sto.Assertion(ref.Type, ref.PrimaryKey, user)
	}

	db := cachedDB(s)
	f := newAccumFetcher(db, retrieve)

	s.Unlock()
	err = fetching(f)
	s.Lock()
	if err != nil {
		return err
	}

	// TODO: trigger w. caller a global sanity check if a is revoked
	// (but try to save as much possible still),
	// or err is a check error
	return commitTo(db, f.fetched)
}
