// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2016-2022 Canonical Ltd
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

	"gopkg.in/tomb.v2"

	"github.com/ddkwork/golibrary/mylog"
	"github.com/snapcore/snapd/asserts"
	"github.com/snapcore/snapd/asserts/snapasserts"
	"github.com/snapcore/snapd/asserts/sysdb"
	"github.com/snapcore/snapd/overlord/snapstate"
	"github.com/snapcore/snapd/overlord/state"
)

// AssertManager is responsible for the enforcement of assertions in
// system states. It manipulates the observed system state to ensure
// nothing in it violates existing assertions, or misses required
// ones.
type AssertManager struct{}

// Manager returns a new assertion manager.
func Manager(s *state.State, runner *state.TaskRunner) (*AssertManager, error) {
	delayedCrossMgrInit()

	runner.AddHandler("validate-snap", doValidateSnap, nil)

	db := mylog.Check2(sysdb.Open())

	s.Lock()
	ReplaceDB(s, db)
	s.Unlock()

	return &AssertManager{}, nil
}

// Ensure implements StateManager.Ensure.
func (m *AssertManager) Ensure() error {
	return nil
}

type cachedDBKey struct{}

// ReplaceDB replaces the assertion database used by the manager.
func ReplaceDB(state *state.State, db *asserts.Database) {
	state.Cache(cachedDBKey{}, db)
}

func cachedDB(s *state.State) *asserts.Database {
	db := s.Cached(cachedDBKey{})
	if db == nil {
		panic("internal error: needing an assertion database before the assertion manager is initialized")
	}
	return db.(*asserts.Database)
}

// DB returns a read-only view of system assertion database.
func DB(s *state.State) asserts.RODatabase {
	return cachedDB(s)
}

// doValidateSnap fetches the relevant assertions for the snap being installed and cross checks them with the snap.
func doValidateSnap(t *state.Task, _ *tomb.Tomb) error {
	st := t.State()
	st.Lock()
	defer st.Unlock()

	snapsup := mylog.Check2(snapstate.TaskSnapSetup(t))

	sha3_384, snapSize := mylog.Check3(asserts.SnapFileSHA3_384(snapsup.SnapPath))

	deviceCtx := mylog.Check2(snapstate.DeviceCtx(st, t, nil))

	modelAs := deviceCtx.Model()
	expectedProv := snapsup.ExpectedProvenance
	mylog.Check(doFetch(st, snapsup.UserID, deviceCtx, nil, func(f asserts.Fetcher) error {
		mylog.Check(snapasserts.FetchSnapAssertions(f, sha3_384, expectedProv))

		// fetch store assertion if available
		if modelAs.Store() != "" {
			mylog.Check(snapasserts.FetchStore(f, modelAs.Store()))
			if notFound, ok := err.(*asserts.NotFoundError); ok {
				if notFound.Type != asserts.StoreType {
					return err
				}
			}

		}

		return nil
	}))
	if notFound, ok := err.(*asserts.NotFoundError); ok {
		if notFound.Type == asserts.SnapRevisionType {
			return fmt.Errorf("cannot verify snap %q, no matching signatures found", snapsup.InstanceName())
		} else {
			return fmt.Errorf("cannot find supported signatures to verify snap %q and its hash (%v)", snapsup.InstanceName(), notFound)
		}
	}

	db := DB(st)
	verifiedRev := mylog.Check2(snapasserts.CrossCheck(snapsup.InstanceName(), sha3_384, expectedProv, snapSize, snapsup.SideInfo, modelAs, db))
	mylog.Check(

		// TODO: trigger a global validity check
		// that will generate the changes to deal with this
		// for things like snap-decl revocation and renames?

		// we have an authorized snap-revision with matching hash for
		// the blob, double check that the snap metadata provenance
		// matches
		snapasserts.CheckProvenanceWithVerifiedRevision(snapsup.SnapPath, verifiedRev))

	// TODO: set DeveloperID from assertions
	return nil
}
