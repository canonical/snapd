// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2026 Canonical Ltd
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

package assertstatetest

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/snapcore/snapd/asserts"
	"github.com/snapcore/snapd/overlord/auth"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/store"
	"github.com/snapcore/snapd/store/storetest"
)

// FakeStore is a minimal in-memory implementation of snapstate.StoreService
// used by assertstate and related tests to exercise the assertion fetch code
// paths without a real store. Assertions are resolved from DB.
type FakeStore struct {
	storetest.Store

	State *state.State
	DB    asserts.RODatabase

	// MaxDeclSupportedFormat, if non-zero, sets the max supported format for
	// snap-declaration assertions during store operations.
	MaxDeclSupportedFormat int
	// MaxValidationSetSupportedFormat, if non-zero, sets the max supported
	// format for validation-set assertions during store operations.
	MaxValidationSetSupportedFormat int

	// RequestedTypes captures the assertion types requested via SnapAction.
	RequestedTypes [][]string
	// Opts captures the last RefreshOptions passed to SnapAction.
	Opts *store.RefreshOptions

	// SnapActionErr, if non-nil, is returned from SnapAction.
	SnapActionErr error
	// DownloadAssertionsErr, if non-nil, is returned from DownloadAssertions.
	DownloadAssertionsErr error
	// AssertionErr, if non-nil, is returned from Assertion.
	AssertionErr error
}

func (sto *FakeStore) pokeStateLock() {
	// the store should be called without the state lock held. Try
	// to acquire it.
	sto.State.Lock()
	sto.State.Unlock()
}

func (sto *FakeStore) Assertion(assertType *asserts.AssertionType, key []string, _ *auth.UserState) (asserts.Assertion, error) {
	if sto.AssertionErr != nil {
		return nil, sto.AssertionErr
	}
	sto.pokeStateLock()

	restore := asserts.MockMaxSupportedFormat(asserts.SnapDeclarationType, sto.MaxDeclSupportedFormat)
	defer restore()

	ref := &asserts.Ref{Type: assertType, PrimaryKey: key}
	return ref.Resolve(sto.DB.Find)
}

func (sto *FakeStore) SeqFormingAssertion(assertType *asserts.AssertionType, sequenceKey []string, sequence int, _ *auth.UserState) (asserts.Assertion, error) {
	sto.pokeStateLock()

	restore := asserts.MockMaxSupportedFormat(asserts.SnapDeclarationType, sto.MaxDeclSupportedFormat)
	defer restore()

	ref := &asserts.AtSequence{
		Type:        assertType,
		SequenceKey: sequenceKey,
		Sequence:    sequence,
		Pinned:      sequence > 0,
	}

	if ref.Sequence <= 0 {
		hdrs, err := asserts.HeadersFromSequenceKey(ref.Type, ref.SequenceKey)
		if err != nil {
			return nil, err
		}
		return sto.DB.FindSequence(ref.Type, hdrs, -1, -1)
	}

	return ref.Resolve(sto.DB.Find)
}

func (sto *FakeStore) SnapAction(_ context.Context, currentSnaps []*store.CurrentSnap, actions []*store.SnapAction, assertQuery store.AssertionQuery, _ *auth.UserState, opts *store.RefreshOptions) ([]store.SnapActionResult, []store.AssertionResult, error) {
	sto.pokeStateLock()

	if len(currentSnaps) != 0 || len(actions) != 0 {
		panic("only assertion query supported")
	}

	toResolve, toResolveSeq, err := assertQuery.ToResolve()
	if err != nil {
		return nil, nil, err
	}

	if sto.SnapActionErr != nil {
		return nil, nil, sto.SnapActionErr
	}

	sto.Opts = opts

	restore := asserts.MockMaxSupportedFormat(asserts.SnapDeclarationType, sto.MaxDeclSupportedFormat)
	defer restore()

	restoreSeq := asserts.MockMaxSupportedFormat(asserts.ValidationSetType, sto.MaxValidationSetSupportedFormat)
	defer restoreSeq()

	reqTypes := make(map[string]bool)
	ares := make([]store.AssertionResult, 0, len(toResolve)+len(toResolveSeq))
	for g, ats := range toResolve {
		urls := make([]string, 0, len(ats))
		for _, at := range ats {
			reqTypes[at.Ref.Type.Name] = true
			a, err := at.Ref.Resolve(sto.DB.Find)
			if err != nil {
				assertQuery.AddError(err, &at.Ref)
				continue
			}
			if a.Revision() > at.Revision {
				urls = append(urls, fmt.Sprintf("/assertions/%s", at.Unique()))
			}
		}
		ares = append(ares, store.AssertionResult{
			Grouping:   asserts.Grouping(g),
			StreamURLs: urls,
		})
	}

	for g, ats := range toResolveSeq {
		urls := make([]string, 0, len(ats))
		for _, at := range ats {
			reqTypes[at.Type.Name] = true
			var a asserts.Assertion
			headers, err := asserts.HeadersFromSequenceKey(at.Type, at.SequenceKey)
			if err != nil {
				return nil, nil, err
			}
			if !at.Pinned {
				a, err = sto.DB.FindSequence(at.Type, headers, -1, asserts.ValidationSetType.MaxSupportedFormat())
			} else {
				a, err = at.Resolve(sto.DB.Find)
			}
			if err != nil {
				assertQuery.AddSequenceError(err, at)
				continue
			}
			storeVs := a.(*asserts.ValidationSet)
			if storeVs.Sequence() > at.Sequence || (storeVs.Sequence() == at.Sequence && storeVs.Revision() >= at.Revision) {
				urls = append(urls, fmt.Sprintf("/assertions/%s/%s", a.Type().Name, strings.Join(a.At().PrimaryKey, "/")))
			}
		}
		ares = append(ares, store.AssertionResult{
			Grouping:   asserts.Grouping(g),
			StreamURLs: urls,
		})
	}

	// behave like the actual SnapAction if there are no results
	if len(ares) == 0 {
		return nil, ares, &store.SnapActionError{
			NoResults: true,
		}
	}

	typeNames := make([]string, 0, len(reqTypes))
	for k := range reqTypes {
		typeNames = append(typeNames, k)
	}
	sort.Strings(typeNames)
	sto.RequestedTypes = append(sto.RequestedTypes, typeNames)

	return nil, ares, nil
}

func (sto *FakeStore) DownloadAssertions(urls []string, b *asserts.Batch, _ *auth.UserState) error {
	sto.pokeStateLock()

	if sto.DownloadAssertionsErr != nil {
		return sto.DownloadAssertionsErr
	}

	resolve := func(ref *asserts.Ref) (asserts.Assertion, error) {
		restore := asserts.MockMaxSupportedFormat(asserts.SnapDeclarationType, sto.MaxDeclSupportedFormat)
		defer restore()

		restoreSeq := asserts.MockMaxSupportedFormat(asserts.ValidationSetType, sto.MaxValidationSetSupportedFormat)
		defer restoreSeq()
		return ref.Resolve(sto.DB.Find)
	}

	for _, u := range urls {
		comps := strings.Split(u, "/")

		if len(comps) < 4 {
			return fmt.Errorf("cannot use URL: %s", u)
		}

		assertType := asserts.Type(comps[2])
		key := comps[3:]
		ref := &asserts.Ref{Type: assertType, PrimaryKey: key}
		a, err := resolve(ref)
		if err != nil {
			return err
		}
		if err := b.Add(a); err != nil {
			return err
		}
	}

	return nil
}
