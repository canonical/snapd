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

	"github.com/snapcore/snapd/asserts"
	"github.com/snapcore/snapd/overlord/snapstate"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/release"
	"github.com/snapcore/snapd/store"
)

const storeGroup = "store assertion"

func bulkRefreshSnapDeclarations(s *state.State, snapStates map[string]*snapstate.SnapState, userID int, deviceCtx snapstate.DeviceContext) error {
	if len(snapStates) > 512 {
		return &bulkAssertionFallbackError{errors.New("internal error: more than supported number of snaps")}
		// TODO: make that work, it's a matter of using many or reusing pools, but keeping only one trail of what is resolved for efficiency
	}

	db := cachedDB(s)

	pool := asserts.NewPool(db, 512)

	for instanceName, snapst := range snapStates {
		sideInfo := snapst.CurrentSideInfo()
		if sideInfo.SnapID == "" {
			continue
		}

		declRef := &asserts.Ref{
			Type:       asserts.SnapDeclarationType,
			PrimaryKey: []string{release.Series, sideInfo.SnapID},
		}
		// update snap-declaration (and prereqs) for the snap,
		// they were originally added at install time
		if err := pool.AddToUpdate(declRef, instanceName); err != nil {
			return fmt.Errorf("cannot prepare snap-declaration refresh for snap %q: %v", instanceName, err)
		}
	}

	modelAs := deviceCtx.Model()

	// fetch store assertion if available
	if modelAs.Store() != "" {
		storeRef := asserts.Ref{
			Type:       asserts.StoreType,
			PrimaryKey: []string{modelAs.Store()},
		}
		if err := pool.AddToUpdate(&storeRef, storeGroup); err != nil {
			if !asserts.IsNotFound(err) {
				return fmt.Errorf("cannot prepare store assertion refresh: %v", err)
			}
			// assertion is not present in the db yet,
			// we'll try to resolve it (fetch it) first
			storeAt := &asserts.AtRevision{
				Ref:      storeRef,
				Revision: asserts.RevisionNotKnown,
			}
			err := pool.AddUnresolved(storeAt, storeGroup)
			if err != nil {
				return fmt.Errorf("cannot prepare store assertion fetching: %v", err)
			}
		}
	}

	err := resolvePool(s, pool, userID, deviceCtx)
	if err != nil {
		if rpe, ok := err.(*resolvePoolError); ok {
			if e := rpe.errors[storeGroup]; asserts.IsNotFound(e) || e == asserts.ErrUnresolved {
				// ignore
				delete(rpe.errors, storeGroup)
			}
			if len(rpe.errors) == 0 {
				return nil
			}
			rpe.message = "cannot refresh snap-declarations for snaps"
		}
	}
	return err
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

func resolvePool(s *state.State, pool *asserts.Pool, userID int, deviceCtx snapstate.DeviceContext) error {
	user, err := userFromUserID(s, userID)
	if err != nil {
		return err
	}
	sto := snapstate.Store(s, deviceCtx)
	db := cachedDB(s)
	unsupported := handleUnsupported(db)

	for {
		// TODO: pass refresh options?
		s.Unlock()
		_, aresults, err := sto.SnapAction(context.TODO(), nil, nil, pool, user, nil)
		s.Lock()
		if err != nil {
			// request fallback on
			//  * unexpected SnapActionErrors or
			//  * unexpected HTTP status of 4xx or 500
			ignore := false
			switch stoErr := err.(type) {
			case *store.SnapActionError:
				if !stoErr.NoResults || len(stoErr.Other) != 0 {
					return &bulkAssertionFallbackError{stoErr}
				}
				// simply no results error, we are likely done
				ignore = true
			case *store.UnexpectedHTTPStatusError:
				if stoErr.StatusCode >= 400 && stoErr.StatusCode <= 500 {
					return &bulkAssertionFallbackError{stoErr}
				}
			}
			if !ignore {
				return err
			}
		}
		if len(aresults) == 0 {
			// everything resolved if no errors
			break
		}

		for _, ares := range aresults {
			b := asserts.NewBatch(unsupported)
			s.Unlock()
			err := sto.DownloadAssertions(ares.StreamURLs, b, user)
			s.Lock()
			if err != nil {
				pool.AddGroupingError(err, ares.Grouping)
				continue
			}
			_, err = pool.AddBatch(b, ares.Grouping)
			if err != nil {
				return err
			}
		}
	}

	pool.CommitTo(db)

	errors := pool.Errors()
	if len(errors) != 0 {
		return &resolvePoolError{errors: errors}
	}

	return nil
}
