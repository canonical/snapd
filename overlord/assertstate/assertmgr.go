// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2016 Canonical Ltd
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

// Package assertstate implements the manager and state aspects responsible
// for the enforcement of assertions in the system and manages the system-wide
// assertion database.
package assertstate

import (
	"fmt"

	"gopkg.in/tomb.v2"

	"github.com/snapcore/snapd/asserts"
	"github.com/snapcore/snapd/asserts/sysdb"
	"github.com/snapcore/snapd/overlord/auth"
	"github.com/snapcore/snapd/overlord/snapstate"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/release"
	//"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/store"
)

// AssertManager is responsible for the enforcement of assertions in
// system states. It manipulates the observed system state to ensure
// nothing in it violates existing assertions, or misses required
// ones.
type AssertManager struct {
	runner *state.TaskRunner
}

// Manager returns a new assertion manager.
func Manager(s *state.State) (*AssertManager, error) {
	runner := state.NewTaskRunner(s)

	runner.AddHandler("fetch-check-snap-assertions", doFetchCheckSnapAssertions, undoFetchCheckSnapAssertions)

	db, err := sysdb.Open()
	if err != nil {
		return nil, err
	}

	s.Lock()
	ReplaceDB(s, db)
	s.Unlock()

	return &AssertManager{runner: runner}, nil
}

// Ensure implements StateManager.Ensure.
func (m *AssertManager) Ensure() error {
	m.runner.Ensure()
	return nil
}

// Wait implements StateManager.Wait.
func (m *AssertManager) Wait() {
	m.runner.Wait()
}

// Stop implements StateManager.Stop.
func (m *AssertManager) Stop() {
	m.runner.Stop()
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

// Add the given assertion to the system assertiond database. Readding the current revision is a no-op.
func Add(s *state.State, a asserts.Assertion) error {
	// TODO: deal together with asserts itself with (cascading) side effects of possible assertion updates
	return cachedDB(s).Add(a)
}

// TODO: snapstate also has this, move to auth, or change a bit the approach now that we have AuthContext in the store?
func userFromUserID(st *state.State, userID int) (*auth.UserState, error) {
	if userID == 0 {
		return nil, nil
	}
	return auth.User(st, userID)
}

// fetch fetches or updates the referenced assertion and all its prerequisites from the store and adds them to the system assertion database. It does not fail if required assertions were already present.
func fetch(s *state.State, ref *asserts.Ref, userID int) error {
	// TODO: once we have a bulk assertion retrieval endpoint this approach will change

	user, err := userFromUserID(s, userID)
	if err != nil {
		return err
	}

	db := cachedDB(s)
	store := snapstate.Store(s)

	s.Unlock()
	defer s.Lock()

	got := []asserts.Assertion{}

	retrieve := func(ref *asserts.Ref) (asserts.Assertion, error) {
		// TODO: ignore errors if already in db?
		return store.Assertion(ref.Type, ref.PrimaryKey, user)
	}

	save := func(a asserts.Assertion) error {
		got = append(got, a)
		return nil
	}

	f := asserts.NewFetcher(db, retrieve, save)

	if err := f.Fetch(ref); err != nil {
		return err
	}

	s.Lock()
	defer s.Unlock()

	for _, a := range got {
		err := db.Add(a)
		if revErr, ok := err.(*asserts.RevisionError); ok {
			if revErr.Current >= a.Revision() {
				// be idempotent
				// system db has already the same or newer
				continue
			}
		}
		// TODO: trigger w. caller a global sanity check if a is revoked
		// (but try to save as much possible still),
		// or err is a check error
		if err != nil {
			return err
		}
	}

	return nil
}

// doFetchCheckSnapAssertions fetches the relevant assertions for the snap being installed and cross checks them with the snap.
func doFetchCheckSnapAssertions(t *state.Task, _ *tomb.Tomb) error {
	t.State().Lock()
	defer t.State().Unlock()

	ss, err := snapstate.TaskSnapSetup(t)
	if err != nil {
		return nil
	}

	sha3_384, snapSize, err := asserts.SnapFileSHA3_384(ss.SnapPath)
	if err != nil {
		return err
	}

	// for now starting from the snap-revision will get us all other relevant assertions
	ref := &asserts.Ref{
		Type:       asserts.SnapRevisionType,
		PrimaryKey: []string{sha3_384},
	}

	err = fetch(t.State(), ref, ss.UserID)
	if err == store.ErrAssertionNotFound {
		// TODO: include which assertion we didn't find
		return fmt.Errorf("cannot find assertions to verify snap %q and its hash", ss.Name())
	}
	if err != nil {
		return err
	}

	// get relevant assertions and and do cross checks
	db := DB(t.State())

	a, err := ref.Resolve(db.Find)
	if err != nil {
		return fmt.Errorf("internal error: cannot find just fetched snap-revision assertion: %v", ref.PrimaryKey)
	}
	snapRev := a.(*asserts.SnapRevision)

	if snapRev.SnapSize() != snapSize {
		return fmt.Errorf("snap %q file does not have expected size according to assertions: %d != %d", ss.Name(), snapSize, snapRev.SnapSize())
	}

	snapID := ss.SideInfo.SnapID

	if snapRev.SnapID() != snapID || snapRev.SnapRevision() != ss.Revision().N {
		return fmt.Errorf("snap %q file hash %s implied snap id %q and revision %d are not the one expected for installing: %q and %s", ss.Name(), sha3_384, snapRev.SnapID(), snapRev.SnapRevision(), snapID, ss.Revision())
	}

	a, err = db.Find(asserts.SnapDeclarationType, map[string]string{
		"series":  release.Series,
		"snap-id": snapID,
	})
	if err != nil {
		return fmt.Errorf("internal error: cannot find just fetched snap declaration: %s", snapID)
	}
	snapDecl := a.(*asserts.SnapDeclaration)

	if snapDecl.SnapName() == "" {
		// TODO: trigger a global sanity check
		// that will generate the changes to deal with this
		return fmt.Errorf("cannot install snap %q with a revoked snap declaration", ss.Name())
	}

	if snapDecl.SnapName() != ss.Name() {
		// TODO: trigger a global sanity check
		// that will generate the changes to deal with this
		return fmt.Errorf("cannot install snap %q that is undergoing a rename to %s", ss.Name(), snapDecl.SnapName())
	}

	// TODO: set DeveloperID from assertions
	return nil
}

func undoFetchCheckSnapAssertions(t *state.Task, _ *tomb.Tomb) error {
	// nothing to do, the assertions that were *actually* added are still true
	return nil
}
