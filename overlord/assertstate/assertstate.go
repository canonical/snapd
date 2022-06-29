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
	"errors"
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

type RefreshAssertionsOptions struct {
	IsAutoRefresh bool
	// IsRefreshOfAllSnaps indicates if assertions are refreshed together with
	// all installed snaps, which means validation set assertions can be refreshed
	// as well. It is implied if IsAutoRefresh is true.
	IsRefreshOfAllSnaps bool
}

// RefreshSnapDeclarations refetches all the current snap declarations and their prerequisites.
func RefreshSnapDeclarations(s *state.State, userID int, opts *RefreshAssertionsOptions) error {
	if opts == nil {
		opts = &RefreshAssertionsOptions{}
	}

	deviceCtx, err := snapstate.DevicePastSeeding(s, nil)
	if err != nil {
		return err
	}

	snapStates, err := snapstate.All(s)
	if err != nil {
		return nil
	}

	err = bulkRefreshSnapDeclarations(s, snapStates, userID, deviceCtx, opts)
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

// PublisherStoreAccount returns the store account information from the publisher assertion.
func PublisherStoreAccount(st *state.State, snapID string) (snap.StoreAccount, error) {
	if snapID == "" {
		return snap.StoreAccount{}, nil
	}

	pubAcct, err := Publisher(st, snapID)
	if err != nil {
		return snap.StoreAccount{}, fmt.Errorf("cannot find publisher details: %v", err)
	}
	return snap.StoreAccount{
		ID:          pubAcct.AccountID(),
		Username:    pubAcct.Username(),
		DisplayName: pubAcct.DisplayName(),
		Validation:  pubAcct.Validation(),
	}, nil
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
		aliasesForApps := make(map[string]string, len(explicitAliases))
		for alias, app := range explicitAliases {
			if _, ok := info.Apps[app]; ok {
				aliasesForApps[alias] = app
			}
		}
		return aliasesForApps, nil
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
	// hook auto refresh of assertions (snap declarations) into snapstate
	snapstate.AutoRefreshAssertions = AutoRefreshAssertions
	// hook retrieving auto-aliases into snapstate logic
	snapstate.AutoAliases = AutoAliases
	// hook the helper for getting enforced validation sets
	snapstate.EnforcedValidationSets = EnforcedValidationSets
	// hook the helper for saving current validation sets to the stack
	snapstate.AddCurrentTrackingToValidationSetsStack = addCurrentTrackingToValidationSetsHistory
	// hook the helper for restoring validation sets tracking from the stack
	snapstate.RestoreValidationSetsTracking = RestoreValidationSetsTracking
}

// AutoRefreshAssertions tries to refresh all assertions
func AutoRefreshAssertions(s *state.State, userID int) error {
	opts := &RefreshAssertionsOptions{IsAutoRefresh: true}
	if err := RefreshSnapDeclarations(s, userID, opts); err != nil {
		return err
	}
	return RefreshValidationSetAssertions(s, userID, opts)
}

// RefreshSnapAssertions tries to refresh all snap-centered assertions
func RefreshSnapAssertions(s *state.State, userID int, opts *RefreshAssertionsOptions) error {
	if opts == nil {
		opts = &RefreshAssertionsOptions{}
	}
	opts.IsAutoRefresh = false
	if err := RefreshSnapDeclarations(s, userID, opts); err != nil {
		return err
	}
	if !opts.IsRefreshOfAllSnaps {
		return nil
	}
	return RefreshValidationSetAssertions(s, userID, opts)
}

// RefreshValidationSetAssertions tries to refresh all validation set
// assertions.
func RefreshValidationSetAssertions(s *state.State, userID int, opts *RefreshAssertionsOptions) error {
	if opts == nil {
		opts = &RefreshAssertionsOptions{}
	}

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

	monitorModeSets := make(map[string]*ValidationSetTracking)
	enforceModeSets := make(map[string]*ValidationSetTracking)
	for vk, vset := range vsets {
		if vset.Mode == Monitor {
			monitorModeSets[vk] = vset
		} else {
			enforceModeSets[vk] = vset
		}
	}

	updateTracking := func(sets map[string]*ValidationSetTracking) error {
		// update validation set tracking state
		for _, vs := range sets {
			if vs.PinnedAt == 0 {
				headers := map[string]string{
					"series":     release.Series,
					"account-id": vs.AccountID,
					"name":       vs.Name,
				}
				db := DB(s)
				as, err := db.FindSequence(asserts.ValidationSetType, headers, -1, asserts.ValidationSetType.MaxSupportedFormat())
				if err != nil {
					return fmt.Errorf("internal error: cannot find assertion %v when refreshing validation-set assertions", headers)
				}
				if vs.Current != as.Sequence() {
					vs.Current = as.Sequence()
					UpdateValidationSet(s, vs)
				}
			}
		}
		return nil
	}

	if err := bulkRefreshValidationSetAsserts(s, monitorModeSets, nil, userID, deviceCtx, opts); err != nil {
		return err
	}
	if err := updateTracking(monitorModeSets); err != nil {
		return err
	}

	checkConflictsAndPresence := func(db *asserts.Database, bs asserts.Backstore) error {
		vsets := snapasserts.NewValidationSets()
		tmpDb := db.WithStackedBackstore(bs)
		for _, vs := range enforceModeSets {
			headers := map[string]string{
				"series":     release.Series,
				"account-id": vs.AccountID,
				"name":       vs.Name,
			}
			var err error
			var as asserts.Assertion
			if vs.PinnedAt > 0 {
				headers["sequence"] = fmt.Sprintf("%d", vs.PinnedAt)
				as, err = tmpDb.Find(asserts.ValidationSetType, headers)
			} else {
				as, err = tmpDb.FindSequence(asserts.ValidationSetType, headers, -1, asserts.ValidationSetType.MaxSupportedFormat())
			}
			if err != nil {
				return fmt.Errorf("internal error: cannot find validation set assertion: %v", err)
			}

			vsass, ok := as.(*asserts.ValidationSet)
			if !ok {
				return fmt.Errorf("internal error: unexpected assertion type %s for %s", vsass.Type().Name, ValidationSetKey(vs.AccountID, vs.Name))
			}
			if err := vsets.Add(vsass); err != nil {
				return fmt.Errorf("internal error: cannot check validation sets conflicts: %v", err)
			}
		}
		if err := vsets.Conflict(); err != nil {
			return err
		}

		snaps, ignoreValidation, err := snapstate.InstalledSnaps(s)
		if err != nil {
			return err
		}
		err = vsets.CheckInstalledSnaps(snaps, ignoreValidation)
		if verr, ok := err.(*snapasserts.ValidationSetsValidationError); ok {
			if len(verr.InvalidSnaps) > 0 || len(verr.MissingSnaps) > 0 {
				return verr
			}
			// ignore wrong revisions
			return nil
		}
		return err
	}

	if err := bulkRefreshValidationSetAsserts(s, enforceModeSets, checkConflictsAndPresence, userID, deviceCtx, opts); err != nil {
		if _, ok := err.(*snapasserts.ValidationSetsConflictError); ok {
			logger.Noticef("cannot refresh to conflicting validation set assertions: %v", err)
			return nil
		}
		if _, ok := err.(*snapasserts.ValidationSetsValidationError); ok {
			logger.Noticef("cannot refresh to validation set assertions that do not satisfy installed snaps: %v", err)
			return nil
		}
		return err
	}
	if err := updateTracking(enforceModeSets); err != nil {
		return err
	}

	return nil
}

// ResolveOptions carries extra options for ValidationSetAssertionForMonitor.
type ResolveOptions struct {
	AllowLocalFallback bool
}

// validationSetAssertionForMonitor tries to fetch or refresh the validation
// set assertion with accountID/name/sequence (sequence is optional) using pool.
// If assertion cannot be fetched but exists locally and opts.AllowLocalFallback
// is set then the local one is returned
func validationSetAssertionForMonitor(st *state.State, accountID, name string, sequence int, pinned bool, userID int, opts *ResolveOptions) (as *asserts.ValidationSet, local bool, err error) {
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

	refreshOpts := &RefreshAssertionsOptions{IsAutoRefresh: false}
	if err := resolvePoolNoFallback(st, pool, nil, userID, deviceCtx, refreshOpts); err != nil {
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

func getSpecificSequenceOrLatest(db *asserts.Database, headers map[string]string) (vs *asserts.ValidationSet, err error) {
	var a asserts.Assertion
	if _, ok := headers["sequence"]; ok {
		a, err = db.Find(asserts.ValidationSetType, headers)
	} else {
		a, err = db.FindSequence(asserts.ValidationSetType, headers, -1, -1)
	}
	if err != nil {
		return nil, err
	}
	vs = a.(*asserts.ValidationSet)
	return vs, nil
}

// validationSetAssertionForEnforce tries to fetch the validation set assertion
// with the given accountID/name/sequence (sequence is optional) using pool and
// checks if it's not in conflict with existing validation sets in enforcing mode
// (all currently tracked validation set assertions get refreshed), and if they
// are valid for installed snaps.
func validationSetAssertionForEnforce(st *state.State, accountID, name string, sequence int, userID int, snaps []*snapasserts.InstalledSnap, ignoreValidation map[string]bool) (vs *asserts.ValidationSet, current int, err error) {
	deviceCtx, err := snapstate.DevicePastSeeding(st, nil)
	if err != nil {
		return nil, 0, err
	}

	opts := &RefreshAssertionsOptions{IsAutoRefresh: false}

	// refresh all currently tracked validation set assertions (this may or may not
	// include the one requested by the caller).
	if err = RefreshValidationSetAssertions(st, userID, opts); err != nil {
		return nil, 0, err
	}

	// try to get existing from the db. It will be the latest one if it was
	// tracked already and thus refreshed via RefreshValidationSetAssertions.
	// Otherwise, it may be a local assertion that was tracked in the past and
	// then forgotten, in which case we need to refresh it explicitly.
	db := cachedDB(st)
	headers := map[string]string{
		"series":     release.Series,
		"account-id": accountID,
		"name":       name,
	}
	if sequence > 0 {
		headers["sequence"] = fmt.Sprintf("%d", sequence)
	}

	pool := asserts.NewPool(db, maxGroups)
	atSeq := &asserts.AtSequence{
		Type:        asserts.ValidationSetType,
		SequenceKey: []string{release.Series, accountID, name},
		Sequence:    sequence,
		Revision:    asserts.RevisionNotKnown,
		Pinned:      sequence > 0,
	}

	vs, err = getSpecificSequenceOrLatest(db, headers)

	checkForConflicts := func() error {
		valsets, err := EnforcedValidationSets(st, vs)
		if err != nil {
			return err
		}
		if err := valsets.Conflict(); err != nil {
			return err
		}
		if err := valsets.CheckInstalledSnaps(snaps, ignoreValidation); err != nil {
			return err
		}
		return nil
	}

	getLatest := func() (int, error) {
		headers := map[string]string{
			"series":     release.Series,
			"account-id": accountID,
			"name":       name,
		}
		a, err := db.FindSequence(asserts.ValidationSetType, headers, -1, -1)
		if err != nil {
			return 0, fmt.Errorf("internal error: %v", err)
		}
		return a.(*asserts.ValidationSet).Sequence(), nil
	}

	// found locally
	if err == nil {
		// check if we were tracking it already; if not, that
		// means we found an old assertion (it was very likely tracked in the
		// past) and we need to update it as it wasn't covered
		// by RefreshValidationSetAssertions.
		var tr ValidationSetTracking
		trerr := GetValidationSet(st, accountID, name, &tr)
		if trerr != nil && !errors.Is(trerr, state.ErrNoState) {
			return nil, 0, trerr
		}
		// not tracked, update the assertion
		if errors.Is(trerr, state.ErrNoState) {
			// update with pool
			atSeq.Sequence = vs.Sequence()
			atSeq.Revision = vs.Revision()
			if err := pool.AddSequenceToUpdate(atSeq, atSeq.Unique()); err != nil {
				return nil, 0, err
			}
		} else {
			// was already tracked, add to validation sets and check
			if err := checkForConflicts(); err != nil {
				return nil, 0, err
			}
			latest, err := getLatest()
			if err != nil {
				return nil, 0, err
			}
			return vs, latest, nil
		}
	} else {
		if !asserts.IsNotFound(err) {
			return nil, 0, err
		}

		// try to resolve with pool
		if err := pool.AddUnresolvedSequence(atSeq, atSeq.Unique()); err != nil {
			return nil, 0, err
		}
	}

	checkBeforeCommit := func(db *asserts.Database, bs asserts.Backstore) error {
		tmpDb := db.WithStackedBackstore(bs)
		// get the resolved validation set assert, add to validation sets and check
		vs, err = getSpecificSequenceOrLatest(tmpDb, headers)
		if err != nil {
			return fmt.Errorf("internal error: cannot find validation set assertion: %v", err)
		}
		if err := checkForConflicts(); err != nil {
			return err
		}
		// all fine, will be committed (along with its prerequisites if any) on
		// return by resolvePoolNoFallback
		return nil
	}

	if err := resolvePoolNoFallback(st, pool, checkBeforeCommit, userID, deviceCtx, opts); err != nil {
		return nil, 0, err
	}

	latest, err := getLatest()
	if err != nil {
		return nil, 0, err
	}
	return vs, latest, err
}

// TryEnforceValidationSets tries to fetch the given validation sets and enforce them (together with currently tracked validation sets) against installed snaps,
// but doesn't update tracking information in case of an error. It may return snapasserts.ValidationSetsValidationError which can be used to install/remove snaps
// as required to satisfy validation sets constraints.
func TryEnforceValidationSets(st *state.State, validationSets []string, userID int, snaps []*snapasserts.InstalledSnap, ignoreValidation map[string]bool) error {
	deviceCtx, err := snapstate.DevicePastSeeding(st, nil)
	if err != nil {
		return err
	}

	db := cachedDB(st)
	pool := asserts.NewPool(db, maxGroups)

	extraVsHeaders := make([]map[string]string, 0, len(validationSets))
	newTracking := make([]*ValidationSetTracking, 0, len(validationSets))

	for _, vsstr := range validationSets {
		accountID, name, sequence, err := snapasserts.ParseValidationSet(vsstr)
		if err != nil {
			return err
		}

		// try to get existing from the db
		headers := map[string]string{
			"series":     release.Series,
			"account-id": accountID,
			"name":       name,
		}
		if sequence > 0 {
			headers["sequence"] = fmt.Sprintf("%d", sequence)
		}
		atSeq := &asserts.AtSequence{
			Type:        asserts.ValidationSetType,
			SequenceKey: []string{release.Series, accountID, name},
			Sequence:    sequence,
			Revision:    asserts.RevisionNotKnown,
			Pinned:      sequence > 0,
		}

		// prepare tracking data, note current is not known yet
		tr := &ValidationSetTracking{
			AccountID: headers["account-id"],
			Name:      headers["name"],
			Mode:      Enforce,
			// may be 0 meaning no pinning
			PinnedAt: sequence,
		}

		extraVsHeaders = append(extraVsHeaders, headers)
		newTracking = append(newTracking, tr)

		vs, err := getSpecificSequenceOrLatest(db, headers)
		// found locally
		if err == nil {
			// update with pool
			atSeq.Sequence = vs.Sequence()
			atSeq.Revision = vs.Revision()
			if err := pool.AddSequenceToUpdate(atSeq, atSeq.Unique()); err != nil {
				return err
			}
		} else {
			if !asserts.IsNotFound(err) {
				return err
			}
			// try to resolve with pool
			if err := pool.AddUnresolvedSequence(atSeq, atSeq.Unique()); err != nil {
				return err
			}
		}
	}

	checkBeforeCommit := func(db *asserts.Database, bs asserts.Backstore) error {
		tmpDb := db.WithStackedBackstore(bs)
		// get the resolved validation set asserts, add to validation sets and check
		var extraVs []*asserts.ValidationSet
		for _, headers := range extraVsHeaders {
			vs, err := getSpecificSequenceOrLatest(tmpDb, headers)
			if err != nil {
				return fmt.Errorf("internal error: cannot find validation set assertion: %v", err)
			}
			extraVs = append(extraVs, vs)
		}

		valsets, err := EnforcedValidationSets(st, extraVs...)
		if err != nil {
			return err
		}
		if err := valsets.Conflict(); err != nil {
			return err
		}
		if err := valsets.CheckInstalledSnaps(snaps, ignoreValidation); err != nil {
			// the returned error may be ValidationSetsValidationError which is normal and means we cannot enforce
			// the new validation sets - the caller should resolve the error and retry.
			return err
		}

		// all fine, will be committed (along with its prerequisites if any) on
		// return by resolvePoolNoFallback
		return nil
	}

	opts := &RefreshAssertionsOptions{}
	if err := resolvePoolNoFallback(st, pool, checkBeforeCommit, userID, deviceCtx, opts); err != nil {
		return err
	}

	// no error, all validation-sets can be enforced, update tracking for all vsets
	for i, headers := range extraVsHeaders {
		// get latest assertion from the db to determine current
		a, err := db.FindSequence(asserts.ValidationSetType, headers, -1, -1)
		if err != nil {
			// this is unexpected since all asserts should be resolved and committed at this point
			return fmt.Errorf("internal error: cannot find validation set assertion: %v", err)
		}
		vs := a.(*asserts.ValidationSet)
		tr := newTracking[i]
		tr.Current = vs.Sequence()
	}
	for _, tr := range newTracking {
		UpdateValidationSet(st, tr)
	}

	return addCurrentTrackingToValidationSetsHistory(st)
}

// EnforceValidationSet tries to fetch the given validation set and enforce it.
// If all validation sets constrains are satisfied, the current validation sets
// tracking state is saved in validation sets history.
func EnforceValidationSet(st *state.State, accountID, name string, sequence, userID int, snaps []*snapasserts.InstalledSnap, ignoreValidation map[string]bool) (*ValidationSetTracking, error) {
	_, current, err := validationSetAssertionForEnforce(st, accountID, name, sequence, userID, snaps, ignoreValidation)
	if err != nil {
		return nil, err
	}

	tr := ValidationSetTracking{
		AccountID: accountID,
		Name:      name,
		Mode:      Enforce,
		// note, sequence may be 0, meaning not pinned.
		PinnedAt: sequence,
		Current:  current,
	}

	UpdateValidationSet(st, &tr)
	err = addCurrentTrackingToValidationSetsHistory(st)
	return &tr, err
}

// MonitorValidationSet tries to fetch the given validation set and monitor it.
// The current validation sets tracking state is saved in validation sets history.
func MonitorValidationSet(st *state.State, accountID, name string, sequence int, userID int) (*ValidationSetTracking, error) {
	pinned := sequence > 0
	opts := ResolveOptions{AllowLocalFallback: true}
	as, local, err := validationSetAssertionForMonitor(st, accountID, name, sequence, pinned, userID, &opts)
	if err != nil {
		return nil, fmt.Errorf("cannot get validation set assertion for %v: %v", ValidationSetKey(accountID, name), err)
	}

	tr := &ValidationSetTracking{
		AccountID: accountID,
		Name:      name,
		Mode:      Monitor,
		// note, Sequence may be 0, meaning not pinned.
		PinnedAt:  sequence,
		Current:   as.Sequence(),
		LocalOnly: local,
	}

	UpdateValidationSet(st, tr)
	return tr, addCurrentTrackingToValidationSetsHistory(st)
}

// TemporaryDB returns a temporary database stacked on top of the assertions
// database. Writing to it will not affect the assertions database.
func TemporaryDB(st *state.State) *asserts.Database {
	db := cachedDB(st)
	return db.WithStackedBackstore(asserts.NewMemoryBackstore())
}
