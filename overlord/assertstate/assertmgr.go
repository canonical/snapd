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
	"errors"
	"fmt"

	"gopkg.in/tomb.v2"

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
	runner.AddHandler("validate-component", doValidateComponent, nil)

	db, err := sysdb.Open()
	if err != nil {
		return nil, err
	}

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

	snapsup, err := snapstate.TaskSnapSetup(t)
	if err != nil {
		return fmt.Errorf("internal error: cannot obtain snap setup: %s", err)
	}

	sha3_384, snapSize, err := asserts.SnapFileSHA3_384(snapsup.SnapPath)
	if err != nil {
		return err
	}

	deviceCtx, err := snapstate.DeviceCtx(st, t, nil)
	if err != nil {
		return err
	}

	modelAs := deviceCtx.Model()
	expectedProv := snapsup.ExpectedProvenance

	err = doFetch(st, snapsup.UserID, deviceCtx, nil, func(f asserts.Fetcher) error {
		if err := snapasserts.FetchSnapAssertions(f, sha3_384, expectedProv); err != nil {
			return err
		}

		// fetch store assertion if available
		if modelAs.Store() != "" {
			err := snapasserts.FetchStore(f, modelAs.Store())
			if notFound, ok := err.(*asserts.NotFoundError); ok {
				if notFound.Type != asserts.StoreType {
					return err
				}
			} else if err != nil {
				return err
			}
		}

		return nil
	})
	if notFound, ok := err.(*asserts.NotFoundError); ok {
		if notFound.Type == asserts.SnapRevisionType {
			return fmt.Errorf("cannot verify snap %q, no matching signatures found", snapsup.InstanceName())
		} else {
			return fmt.Errorf("cannot find supported signatures to verify snap %q and its hash (%v)", snapsup.InstanceName(), notFound)
		}
	}
	if err != nil {
		return err
	}

	db := DB(st)
	verifiedRev, err := snapasserts.CrossCheck(snapsup.InstanceName(), sha3_384, expectedProv, snapSize, snapsup.SideInfo, modelAs, db)
	if err != nil {
		// TODO: trigger a global validity check
		// that will generate the changes to deal with this
		// for things like snap-decl revocation and renames?
		return err
	}

	// we have an authorized snap-revision with matching hash for
	// the blob, double check that the snap metadata provenance
	// matches
	if err := snapasserts.CheckProvenanceWithVerifiedRevision(snapsup.SnapPath, verifiedRev); err != nil {
		return err
	}

	// TODO: set DeveloperID from assertions
	return nil
}

func doValidateComponent(t *state.Task, _ *tomb.Tomb) error {
	st := t.State()
	st.Lock()
	defer st.Unlock()

	compsup, snapsup, err := snapstate.TaskComponentSetup(t)
	if err != nil {
		return fmt.Errorf("internal error: cannot obtain snap setup: %s", err)
	}

	sha3_384, compSize, err := asserts.SnapFileSHA3_384(compsup.CompPath)
	if err != nil {
		return err
	}

	deviceCtx, err := snapstate.DeviceCtx(st, t, nil)
	if err != nil {
		return err
	}

	modelAs := deviceCtx.Model()

	// the provenance of the snap is the expected provenance for the component
	expectedProv := snapsup.ExpectedProvenance

	err = doFetch(st, snapsup.UserID, deviceCtx, nil, func(f asserts.Fetcher) error {
		if err := snapasserts.FetchComponentAssertions(f, snapsup.SideInfo, compsup.CompSideInfo, sha3_384, expectedProv); err != nil {
			return err
		}

		// TODO: do we want this part here? it happens in doValidateSnap
		// fetch store assertion if available
		if modelAs.Store() != "" {
			err := snapasserts.FetchStore(f, modelAs.Store())
			if notFound, ok := err.(*asserts.NotFoundError); ok {
				if notFound.Type != asserts.StoreType {
					return err
				}
			} else if err != nil {
				return err
			}
		}

		return nil
	})
	if errors.Is(err, &asserts.NotFoundError{}) {
		return fmt.Errorf("cannot find supported signatures to verify component %q and its hash (%v)", compsup.ComponentName(), err)
	}
	if err != nil {
		return err
	}

	db := DB(st)
	err = snapasserts.CrossCheckResource(compsup.ComponentName(), sha3_384, expectedProv, compSize, compsup.CompSideInfo, snapsup.SideInfo, modelAs, db)
	if err != nil {
		return err
	}

	// TODO: check the provenance stored inside the component blob against what
	// we expect from the assertion (similar to
	// snapasserts.CheckProvenanceWithVerifiedRevision)

	return nil
}
