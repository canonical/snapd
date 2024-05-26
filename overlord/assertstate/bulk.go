// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2020 Canonical Ltd
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
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/ddkwork/golibrary/mylog"
	"github.com/snapcore/snapd/asserts"
	"github.com/snapcore/snapd/asserts/snapasserts"
	"github.com/snapcore/snapd/overlord/snapstate"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/release"
	"github.com/snapcore/snapd/store"
)

const storeGroup = "store assertion"

// maxGroups is the maximum number of assertion groups we set with the
// asserts.Pool used to refresh snap assertions, it corresponds
// roughly to for how many snaps we will request assertions in
// in one /v2/snaps/refresh request.
// Given that requesting assertions for ~500 snaps together with no
// updates can take around 900ms-1s, conservatively set it to half of
// that. Most systems should be done in one request anyway.
var maxGroups = 256

func bulkRefreshSnapDeclarations(s *state.State, snapStates map[string]*snapstate.SnapState, userID int, deviceCtx snapstate.DeviceContext, opts *RefreshAssertionsOptions) error {
	db := cachedDB(s)

	pool := asserts.NewPool(db, maxGroups)

	var mergedRPErr *resolvePoolError
	tryResolvePool := func() error {
		mylog.Check(resolvePool(s, pool, nil, userID, deviceCtx, opts))
		if rpe, ok := err.(*resolvePoolError); ok {
			if mergedRPErr == nil {
				mergedRPErr = rpe
			} else {
				mergedRPErr.merge(rpe)
			}
			return nil
		}
		return err
	}

	c := 0
	for instanceName, snapst := range snapStates {
		sideInfo := snapst.CurrentSideInfo()
		if sideInfo.SnapID == "" {
			continue
		}

		declRef := &asserts.Ref{
			Type:       asserts.SnapDeclarationType,
			PrimaryKey: []string{release.Series, sideInfo.SnapID},
		}
		mylog.Check(
			// update snap-declaration (and prereqs) for the snap,
			// they were originally added at install time
			pool.AddToUpdate(declRef, instanceName))

		c++
		if c%maxGroups == 0 {
			mylog.Check(
				// we have exhausted max groups, resolve
				// what we setup so far and then clear groups
				// to reuse the pool
				tryResolvePool())
			mylog.Check(pool.ClearGroups())
			// this shouldn't happen but if it
			// does fallback

		}
	}

	modelAs := deviceCtx.Model()

	// fetch store assertion if available
	if modelAs.Store() != "" {
		storeRef := asserts.Ref{
			Type:       asserts.StoreType,
			PrimaryKey: []string{modelAs.Store()},
		}
		mylog.Check(pool.AddToUpdate(&storeRef, storeGroup))

		// assertion is not present in the db yet,
		// we'll try to resolve it (fetch it) first

	}
	mylog.Check(tryResolvePool())

	if mergedRPErr != nil {
		if e := mergedRPErr.errors[storeGroup]; errors.Is(e, &asserts.NotFoundError{}) || e == asserts.ErrUnresolved {
			// ignore
			delete(mergedRPErr.errors, storeGroup)
		}
		if len(mergedRPErr.errors) == 0 {
			return nil
		}
		mergedRPErr.message = "cannot refresh snap-declarations for snaps"
		return mergedRPErr
	}

	return nil
}

func bulkRefreshValidationSetAsserts(s *state.State, vsets map[string]*ValidationSetTracking, beforeCommitChecker func(*asserts.Database, asserts.Backstore) error, userID int, deviceCtx snapstate.DeviceContext, opts *RefreshAssertionsOptions) error {
	db := cachedDB(s)
	pool := asserts.NewPool(db, maxGroups)

	ignoreNotFound := make(map[string]bool)

	for _, vs := range vsets {
		var atSeq *asserts.AtSequence
		if vs.PinnedAt > 0 {
			// pinned to specific sequence, update to latest revision for same
			// sequence.
			atSeq = &asserts.AtSequence{
				Type:        asserts.ValidationSetType,
				SequenceKey: []string{release.Series, vs.AccountID, vs.Name},
				Sequence:    vs.PinnedAt,
				Pinned:      true,
			}
		} else {
			// not pinned, update to latest sequence
			atSeq = &asserts.AtSequence{
				Type:        asserts.ValidationSetType,
				SequenceKey: []string{release.Series, vs.AccountID, vs.Name},
				Sequence:    vs.Current,
			}
		}
		// every sequence to resolve has own group
		group := atSeq.Unique()
		if vs.LocalOnly {
			ignoreNotFound[group] = true
		}
		mylog.Check(pool.AddSequenceToUpdate(atSeq, group))

	}
	mylog.Check(resolvePoolNoFallback(s, pool, beforeCommitChecker, userID, deviceCtx, opts))
	if err == nil {
		return nil
	}

	if _, ok := err.(*snapasserts.ValidationSetsConflictError); ok {
		return err
	}
	if _, ok := err.(*snapasserts.ValidationSetsValidationError); ok {
		return err
	}

	if rerr, ok := err.(*resolvePoolError); ok {
		// ignore resolving errors for validation sets that are local only (no
		// assertion in the store).
		for group := range ignoreNotFound {
			if e := rerr.errors[group]; errors.Is(e, &asserts.NotFoundError{}) || e == asserts.ErrUnresolved {
				delete(rerr.errors, group)
			}
		}
		if len(rerr.errors) == 0 {
			return nil
		}
	}

	return fmt.Errorf("cannot refresh validation set assertions: %v", err)
}

// marker error to request falling back to the old implemention for assertion
// refreshes
type bulkAssertionFallbackError struct {
	err error
}

func (e *bulkAssertionFallbackError) Error() string {
	return fmt.Sprintf("unsuccessful bulk assertion refresh, fallback: %v", e.err)
}

type resolvePoolError struct {
	message string
	// errors maps groups to errors
	errors map[string]error
}

func (rpe *resolvePoolError) merge(rpe1 *resolvePoolError) {
	// we expect usually rpe and rpe1 errors to be disjunct, but is also
	// ok for rpe1 errors to win
	for k, e := range rpe1.errors {
		rpe.errors[k] = e
	}
}

func (rpe *resolvePoolError) Error() string {
	message := rpe.message
	if message == "" {
		message = "cannot fetch and resolve assertions"
	}
	s := make([]string, 0, 1+len(rpe.errors))
	s = append(s, fmt.Sprintf("%s:", message))
	groups := make([]string, 0, len(rpe.errors))
	for g := range rpe.errors {
		groups = append(groups, g)
	}
	sort.Strings(groups)
	for _, g := range groups {
		s = append(s, fmt.Sprintf(" - %s: %v", g, rpe.errors[g]))
	}
	return strings.Join(s, "\n")
}

func resolvePool(s *state.State, pool *asserts.Pool, checkBeforeCommit func(*asserts.Database, asserts.Backstore) error, userID int, deviceCtx snapstate.DeviceContext, opts *RefreshAssertionsOptions) error {
	user := mylog.Check2(userFromUserID(s, userID))

	sto := snapstate.Store(s, deviceCtx)
	db := cachedDB(s)
	unsupported := handleUnsupported(db)

	for {
		storeOpts := &store.RefreshOptions{Scheduled: opts.IsAutoRefresh}
		s.Unlock()
		_, aresults := mylog.Check3(sto.SnapAction(context.TODO(), nil, nil, pool, user, storeOpts))
		s.Lock()

		// request fallback on
		//  * unexpected SnapActionErrors or
		//  * unexpected HTTP status of 4xx or 500

		// simply no results error, we are likely done

		if len(aresults) == 0 {
			// everything resolved if no errors
			break
		}

		for _, ares := range aresults {
			b := asserts.NewBatch(unsupported)
			s.Unlock()
			mylog.Check(sto.DownloadAssertions(ares.StreamURLs, b, user))
			s.Lock()

			_ = mylog.Check2(pool.AddBatch(b, ares.Grouping))

		}
	}

	if checkBeforeCommit != nil {
		mylog.Check(checkBeforeCommit(db, pool.Backstore()))
	}
	pool.CommitTo(db)

	errors := pool.Errors()
	if len(errors) != 0 {
		return &resolvePoolError{errors: errors}
	}

	return nil
}

func resolvePoolNoFallback(s *state.State, pool *asserts.Pool, checkBeforeCommit func(*asserts.Database, asserts.Backstore) error, userID int, deviceCtx snapstate.DeviceContext, opts *RefreshAssertionsOptions) error {
	mylog.Check(resolvePool(s, pool, checkBeforeCommit, userID, deviceCtx, opts))

	// no fallback, report inner error.

	return err
}
