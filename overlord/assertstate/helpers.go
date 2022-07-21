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

// handleUnsupported behaves as a fallback in case of bugs, we do ask
// the store to filter unsupported formats!
func handleUnsupported(db asserts.RODatabase) func(ref *asserts.Ref, unsupportedErr error) error {
	return func(ref *asserts.Ref, unsupportedErr error) error {
		if _, err := ref.Resolve(db.Find); err != nil {
			// nothing there yet or any other error
			return unsupportedErr
		}
		// we keep the old one, but log the issue
		logger.Noticef("Cannot update assertion %v: %v", ref, unsupportedErr)
		return nil
	}
}

func doFetch(s *state.State, userID int, deviceCtx snapstate.DeviceContext, fetching func(asserts.Fetcher) error) error {
	// TODO: once we have a bulk assertion retrieval endpoint this approach will change

	db := cachedDB(s)

	b := asserts.NewBatch(handleUnsupported(db))

	user, err := userFromUserID(s, userID)
	if err != nil {
		return err
	}

	sto := snapstate.Store(s, deviceCtx)

	retrieve := func(ref *asserts.Ref) (asserts.Assertion, error) {
		// TODO: ignore errors if already in db?
		return sto.Assertion(ref.Type, ref.PrimaryKey, user)
	}

	s.Unlock()
	err = b.Fetch(db, retrieve, fetching)
	s.Lock()
	if err != nil {
		return err
	}

	// TODO: trigger w. caller a global validity check if a is revoked
	// (but try to save as much possible still),
	// or err is a check error
	return b.CommitTo(db, nil)
}
