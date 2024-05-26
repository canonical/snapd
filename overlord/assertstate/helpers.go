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
	"github.com/ddkwork/golibrary/mylog"
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
		mylog.Check2(ref.Resolve(db.Find))
		// nothing there yet or any other error

		// we keep the old one, but log the issue
		logger.Noticef("Cannot update assertion %v: %v", ref, unsupportedErr)
		return nil
	}
}

// doFetch fetches and save assertions. If a batch is passed then it's not committed.
// If no batch is passed, one is created and committed.
func doFetch(s *state.State, userID int, deviceCtx snapstate.DeviceContext, batch *asserts.Batch, fetching func(asserts.Fetcher) error) error {
	// TODO: once we have a bulk assertion retrieval endpoint this approach will change
	db := cachedDB(s)

	// don't commit batch if it was passed in since caller might want to do it later
	var commitBatch bool
	if batch == nil {
		batch = asserts.NewBatch(handleUnsupported(db))
		commitBatch = true
	}

	user := mylog.Check2(userFromUserID(s, userID))

	sto := snapstate.Store(s, deviceCtx)

	retrieve := func(ref *asserts.Ref) (asserts.Assertion, error) {
		// TODO: ignore errors if already in db?
		return sto.Assertion(ref.Type, ref.PrimaryKey, user)
	}

	s.Unlock()
	mylog.Check(batch.Fetch(db, retrieve, fetching))
	s.Lock()

	// TODO: trigger w. caller a global validity check if a is revoked
	// (but try to save as much possible still), or err is a check error
	if commitBatch {
		return batch.CommitTo(db, nil)
	}

	return nil
}
