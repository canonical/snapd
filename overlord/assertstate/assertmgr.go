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

	runner.AddHandler("prefetch-assertions", doPrefetchAssertions, undoPrefetchAssertions)
	// TODO: check-assertions handlers

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

// TODO: snapstate also has this, move to auth?
func userFromUserID(st *state.State, userID int) (*auth.UserState, error) {
	if userID == 0 {
		return nil, nil
	}
	return auth.User(st, userID)
}

// Fetch fetches or updates the referenced assertion and all its prerequisites from the store and adds them to the system assertion database. It does not fail if required assertions were already present.
func Fetch(s *state.State, ref *asserts.Ref, userID int) error {
	// TODO: once we have a bulk assertion retrieval endpoint the internal approach might change

	user, err := userFromUserID(s, userID)
	if err != nil {
		return err
	}

	db := cachedDB(s)
	store := snapstate.Store(s)

	s.Unlock()
	defer s.Lock()

	got := ([]asserts.Assertion)(nil)

	retrieve := func(ref *asserts.Ref) (asserts.Assertion, error) {
		// TODO: ignore errors if already in db?
		return store.Assertion(ref.Type, ref.PrimaryKey, user)
	}

	save := func(a asserts.Assertion) error {
		got = append(got, a)
		return nil
	}

	f := &fetcher{
		retrieve: retrieve,
		save:     save,
	}
	f.init(db)

	err = f.doFetch(ref)
	if err != nil {
		return err
	}

	s.Lock()
	defer s.Unlock()

	// TODO: deal together with asserts itself with cascading side effects of possible assertion updates

	for _, a := range got {
		err := db.Add(a)
		if revErr, ok := err.(*asserts.RevisionError); ok {
			if revErr.Current >= a.Revision() {
				// system db has already the same or newer
				continue
			}
		}
		if err != nil {
			return err
		}
	}

	return nil
}

type fetchProgress int

const (
	fetchNotSeen fetchProgress = iota
	fetchRetrieved
	fetchSaved
)

type fetcher struct {
	db asserts.RODatabase

	retrieve func(*asserts.Ref) (asserts.Assertion, error) // can ignore errors as needed
	save     func(asserts.Assertion) error

	fetched map[string]fetchProgress
}

func (f *fetcher) init(db asserts.RODatabase) {
	f.db = db
	f.fetched = make(map[string]fetchProgress)
}

func (f *fetcher) doFetch(ref *asserts.Ref) error {
	_, err := ref.Resolve(f.db.FindTrusted)
	if err == nil {
		return nil
	}
	if err != asserts.ErrNotFound {
		return err
	}
	u := ref.Unique()
	switch f.fetched[u] {
	case fetchSaved:
		return nil // nothing to do
	case fetchRetrieved:
		return fmt.Errorf("internal error: circular assertions are not expected: %v", ref)
	}
	a, err := f.retrieve(ref)
	if err != nil {
		return err
	}
	f.fetched[u] = fetchRetrieved
	for _, preref := range a.Prerequisites() {
		err := f.doFetch(preref)
		if err != nil {
			return err
		}
	}
	keyRef := &asserts.Ref{
		Type:       asserts.AccountKeyType,
		PrimaryKey: []string{a.SignKeyID()},
	}
	err = f.doFetch(keyRef)
	if err != nil {
		return err
	}
	if err := f.save(a); err != nil {
		return err
	}
	f.fetched[u] = fetchSaved
	return nil
}

// doPrefetchAssertions prefetches the relevant assertions for the snap being installed.
func doPrefetchAssertions(t *state.Task, _ *tomb.Tomb) error {
	// XXX: WIP
	t.State().Lock()
	defer t.State().Unlock()

	ss, err := snapstate.TaskSnapSetup(t)
	if err != nil {
		return nil
	}

	if ss.SideInfo.SnapID == "" {
		return fmt.Errorf("cannot prefetch assertions for local snap %q", ss.Name())
	}

	digest, err := ss.SideInfo.SnapDigest()
	if err != nil {
		return fmt.Errorf("cannot use snap %q digest: %v", ss.Name, err)
	}

	// for now starting from the snap-revision will get us all other relevant assertions
	ref := &asserts.Ref{
		Type:       asserts.SnapRevisionType,
		PrimaryKey: []string{release.Series, ss.SideInfo.SnapID, digest},
	}

	return Fetch(t.State(), ref, ss.UserID)
}

func undoPrefetchAssertions(t *state.Task, _ *tomb.Tomb) error {
	// nothing to do, the assertions that were *actually* added are still true
	return nil
}
