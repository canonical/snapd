// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2016-2019 Canonical Ltd
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
	"strings"

	"github.com/snapcore/snapd/asserts"
	"github.com/snapcore/snapd/asserts/snapasserts"
	"github.com/snapcore/snapd/httputil"
	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/overlord/snapstate"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/release"
	"github.com/snapcore/snapd/snap"
)

// Add the given assertion to the system assertion database.
func Add(s *state.State, a asserts.Assertion) error {
	// TODO: deal together with asserts itself with (cascading) side effects of possible assertion updates
	return cachedDB(s).Add(a)
}

// AddBatch adds the given assertion batch to the system assertion database.
func AddBatch(s *state.State, batch *asserts.Batch, opts *asserts.CommitOptions) error {
	return batch.CommitTo(cachedDB(s), opts)
}

func findError(format string, ref *asserts.Ref, err error) error {
	if asserts.IsNotFound(err) {
		return fmt.Errorf(format, ref)
	} else {
		return fmt.Errorf(format+": %v", ref, err)
	}
}

// RefreshSnapDeclarations refetches all the current snap declarations and their prerequisites.
func RefreshSnapDeclarations(s *state.State, userID int) error {
	deviceCtx, err := snapstate.DevicePastSeeding(s, nil)
	if err != nil {
		return err
	}

	snapStates, err := snapstate.All(s)
	if err != nil {
		return nil
	}

	err = bulkRefreshSnapDeclarations(s, snapStates, userID, deviceCtx)
	if err == nil {
		// done
		return nil
	}
	if _, ok := err.(*bulkAssertionFallbackError); !ok {
		// not an error that indicates the server rejecting/failing
		// the bulk request itself
		return err
	}
	logger.Noticef("bulk refresh of snap-declarations failed, falling back to one-by-one assertion fetching: %v", err)

	modelAs := deviceCtx.Model()

	fetching := func(f asserts.Fetcher) error {
		for instanceName, snapst := range snapStates {
			sideInfo := snapst.CurrentSideInfo()
			if sideInfo.SnapID == "" {
				continue
			}
			if err := snapasserts.FetchSnapDeclaration(f, sideInfo.SnapID); err != nil {
				if notRetried, ok := err.(*httputil.PersistentNetworkError); ok {
					return notRetried
				}
				return fmt.Errorf("cannot refresh snap-declaration for %q: %v", instanceName, err)
			}
		}

		// fetch store assertion if available
		if modelAs.Store() != "" {
			err := snapasserts.FetchStore(f, modelAs.Store())
			if err != nil && !asserts.IsNotFound(err) {
				return err
			}
		}

		return nil
	}
	return doFetch(s, userID, deviceCtx, fetching)
}

type refreshControlError struct {
	errs []error
}

func (e *refreshControlError) Error() string {
	if len(e.errs) == 1 {
		return e.errs[0].Error()
	}
	l := []string{""}
	for _, e := range e.errs {
		l = append(l, e.Error())
	}
	return fmt.Sprintf("refresh control errors:%s", strings.Join(l, "\n - "))
}

// ValidateRefreshes validates the refresh candidate revisions represented by
// the snapInfos, looking for the needed refresh control validation assertions,
// it returns a validated subset in validated and a summary error if not all
// candidates validated. ignoreValidation is a set of snap-instance-names that
// should not be gated.
func ValidateRefreshes(s *state.State, snapInfos []*snap.Info, ignoreValidation map[string]bool, userID int, deviceCtx snapstate.DeviceContext) (validated []*snap.Info, err error) {
	// maps gated snap-ids to gating snap-ids
	controlled := make(map[string][]string)
	// maps gating snap-ids to their snap names
	gatingNames := make(map[string]string)

	db := DB(s)
	snapStates, err := snapstate.All(s)
	if err != nil {
		return nil, err
	}
	for instanceName, snapst := range snapStates {
		info, err := snapst.CurrentInfo()
		if err != nil {
			return nil, err
		}
		if info.SnapID == "" {
			continue
		}
		gatingID := info.SnapID
		if gatingNames[gatingID] != "" {
			continue
		}
		a, err := db.Find(asserts.SnapDeclarationType, map[string]string{
			"series":  release.Series,
			"snap-id": gatingID,
		})
		if err != nil {
			return nil, fmt.Errorf("internal error: cannot find snap declaration for installed snap %q: %v", instanceName, err)
		}
		decl := a.(*asserts.SnapDeclaration)
		control := decl.RefreshControl()
		if len(control) == 0 {
			continue
		}
		gatingNames[gatingID] = decl.SnapName()
		for _, gatedID := range control {
			controlled[gatedID] = append(controlled[gatedID], gatingID)
		}
	}

	var errs []error
	for _, candInfo := range snapInfos {
		if ignoreValidation[candInfo.InstanceName()] {
			validated = append(validated, candInfo)
			continue
		}
		gatedID := candInfo.SnapID
		gating := controlled[gatedID]
		if len(gating) == 0 { // easy case, no refresh control
			validated = append(validated, candInfo)
			continue
		}

		var validationRefs []*asserts.Ref

		fetching := func(f asserts.Fetcher) error {
			for _, gatingID := range gating {
				valref := &asserts.Ref{
					Type:       asserts.ValidationType,
					PrimaryKey: []string{release.Series, gatingID, gatedID, candInfo.Revision.String()},
				}
				err := f.Fetch(valref)
				if notFound, ok := err.(*asserts.NotFoundError); ok && notFound.Type == asserts.ValidationType {
					return fmt.Errorf("no validation by %q", gatingNames[gatingID])
				}
				if err != nil {
					return fmt.Errorf("cannot find validation by %q: %v", gatingNames[gatingID], err)
				}
				validationRefs = append(validationRefs, valref)
			}
			return nil
		}
		err := doFetch(s, userID, deviceCtx, fetching)
		if err != nil {
			errs = append(errs, fmt.Errorf("cannot refresh %q to revision %s: %v", candInfo.InstanceName(), candInfo.Revision, err))
			continue
		}

		var revoked *asserts.Validation
		for _, valref := range validationRefs {
			a, err := valref.Resolve(db.Find)
			if err != nil {
				return nil, findError("internal error: cannot find just fetched %v", valref, err)
			}
			if val := a.(*asserts.Validation); val.Revoked() {
				revoked = val
				break
			}
		}
		if revoked != nil {
			errs = append(errs, fmt.Errorf("cannot refresh %q to revision %s: validation by %q (id %q) revoked", candInfo.InstanceName(), candInfo.Revision, gatingNames[revoked.SnapID()], revoked.SnapID()))
			continue
		}

		validated = append(validated, candInfo)
	}

	if errs != nil {
		return validated, &refreshControlError{errs}
	}

	return validated, nil
}

// BaseDeclaration returns the base-declaration assertion with policies governing all snaps.
func BaseDeclaration(s *state.State) (*asserts.BaseDeclaration, error) {
	// TODO: switch keeping this in the DB and have it revisioned/updated
	// via the store
	baseDecl := asserts.BuiltinBaseDeclaration()
	if baseDecl == nil {
		return nil, &asserts.NotFoundError{Type: asserts.BaseDeclarationType}
	}
	return baseDecl, nil
}

// SnapDeclaration returns the snap-declaration for the given snap-id if it is present in the system assertion database.
func SnapDeclaration(s *state.State, snapID string) (*asserts.SnapDeclaration, error) {
	db := DB(s)
	a, err := db.Find(asserts.SnapDeclarationType, map[string]string{
		"series":  release.Series,
		"snap-id": snapID,
	})
	if err != nil {
		return nil, err
	}
	return a.(*asserts.SnapDeclaration), nil
}

// Publisher returns the account assertion for publisher of the given snap-id if it is present in the system assertion database.
func Publisher(s *state.State, snapID string) (*asserts.Account, error) {
	db := DB(s)
	a, err := db.Find(asserts.SnapDeclarationType, map[string]string{
		"series":  release.Series,
		"snap-id": snapID,
	})
	if err != nil {
		return nil, err
	}
	snapDecl := a.(*asserts.SnapDeclaration)
	a, err = db.Find(asserts.AccountType, map[string]string{
		"account-id": snapDecl.PublisherID(),
	})
	if err != nil {
		return nil, fmt.Errorf("internal error: cannot find account assertion for the publisher of snap %q: %v", snapDecl.SnapName(), err)
	}
	return a.(*asserts.Account), nil
}

// Store returns the store assertion with the given name/id if it is
// present in the system assertion database.
func Store(s *state.State, store string) (*asserts.Store, error) {
	db := DB(s)
	a, err := db.Find(asserts.StoreType, map[string]string{
		"store": store,
	})
	if err != nil {
		return nil, err
	}
	return a.(*asserts.Store), nil
}

// AutoAliases returns the explicit automatic aliases alias=>app mapping for the given installed snap.
func AutoAliases(s *state.State, info *snap.Info) (map[string]string, error) {
	if info.SnapID == "" {
		// without declaration
		return nil, nil
	}
	decl, err := SnapDeclaration(s, info.SnapID)
	if err != nil {
		return nil, fmt.Errorf("internal error: cannot find snap-declaration for installed snap %q: %v", info.InstanceName(), err)
	}
	explicitAliases := decl.Aliases()
	if len(explicitAliases) != 0 {
		return explicitAliases, nil
	}
	// XXX: old header fallback, just to keep edge working while we fix the
	// store, to remove before next release!
	oldAutoAliases := decl.AutoAliases()
	if len(oldAutoAliases) == 0 {
		return nil, nil
	}
	res := make(map[string]string, len(oldAutoAliases))
	for _, alias := range oldAutoAliases {
		app := info.LegacyAliases[alias]
		if app == nil {
			// not a known alias anymore or yet, skip
			continue

		}
		res[alias] = app.Name
	}
	return res, nil
}

func delayedCrossMgrInit() {
	// hook validation of refreshes into snapstate logic
	snapstate.ValidateRefreshes = ValidateRefreshes
	// hook auto refresh of assertions into snapstate
	snapstate.AutoRefreshAssertions = AutoRefreshAssertions
	// hook retrieving auto-aliases into snapstate logic
	snapstate.AutoAliases = AutoAliases
}

// AutoRefreshAssertions tries to refresh all assertions
func AutoRefreshAssertions(s *state.State, userID int) error {
	return RefreshSnapDeclarations(s, userID)
}

// RefreshValidationSetAssertions tries to refresh all validation set
// assertions.
func RefreshValidationSetAssertions(s *state.State, userID int) error {
	deviceCtx, err := snapstate.DevicePastSeeding(s, nil)
	if err != nil {
		return err
	}

	vsets, err := ValidationSets(s)
	if err != nil {
		return err
	}
	if len(vsets) == 0 {
		return nil
	}

	return bulkRefreshValidationSetAsserts(s, vsets, userID, deviceCtx)
}

// ResolveOptions carries extra options for ValidationSetAssertionForMonitor.
type ResolveOptions struct {
	AllowLocalFallback bool
}

// ValidationSetAssertionForMonitor tries to fetch or refresh the validation
// set assertion with accountID/name/sequence (sequence is optional) using pool.
// If assertion cannot be fetched but exists locally and opts.AllowLocalFallback
// is set then the local one is returned
func ValidationSetAssertionForMonitor(st *state.State, accountID, name string, sequence int, pinned bool, userID int, opts *ResolveOptions) (as *asserts.ValidationSet, local bool, err error) {
	if opts == nil {
		opts = &ResolveOptions{}
	}
	deviceCtx, err := snapstate.DevicePastSeeding(st, nil)
	if err != nil {
		return nil, false, err
	}

	var vs asserts.Assertion
	headers := map[string]string{
		"series":     release.Series,
		"account-id": accountID,
		"name":       name,
	}

	db := cachedDB(st)

	// try to get existing one from db
	if sequence > 0 {
		headers["sequence"] = fmt.Sprintf("%d", sequence)
		vs, err = db.Find(asserts.ValidationSetType, headers)
	} else {
		// find latest
		vs, err = db.FindSequence(asserts.ValidationSetType, headers, -1, -1)
	}
	if err != nil && !asserts.IsNotFound(err) {
		return nil, false, err
	}
	if err == nil {
		as = vs.(*asserts.ValidationSet)
	}

	// try to resolve or update with pool
	pool := asserts.NewPool(db, maxGroups)
	atSeq := &asserts.AtSequence{
		Type:        asserts.ValidationSetType,
		SequenceKey: []string{release.Series, accountID, name},
		Sequence:    sequence,
		Pinned:      pinned,
	}
	if as != nil {
		atSeq.Revision = as.Revision()
	} else {
		atSeq.Revision = asserts.RevisionNotKnown
	}

	// resolve if not found locally, otherwise add for update
	if as == nil {
		if err := pool.AddUnresolvedSequence(atSeq, atSeq.Unique()); err != nil {
			return nil, false, err
		}
	} else {
		atSeq.Sequence = as.Sequence()
		// found locally, try to update
		atSeq.Revision = as.Revision()
		if err := pool.AddSequenceToUpdate(atSeq, atSeq.Unique()); err != nil {
			return nil, false, err
		}
	}

	if err := resolvePoolNoFallback(st, pool, userID, deviceCtx); err != nil {
		rerr, ok := err.(*resolvePoolError)
		if ok && as != nil && opts.AllowLocalFallback {
			if e := rerr.errors[atSeq.Unique()]; asserts.IsNotFound(e) {
				// fallback: support the scenario of local assertion (snap ack)
				// not available in the store.
				return as, true, nil
			}
		}
		return nil, false, err
	}

	// fetch the requested assertion again
	if pinned {
		vs, err = db.Find(asserts.ValidationSetType, headers)
	} else {
		vs, err = db.FindSequence(asserts.ValidationSetType, headers, -1, asserts.ValidationSetType.MaxSupportedFormat())
	}
	if err == nil {
		as = vs.(*asserts.ValidationSet)
	}
	return as, false, err
}
