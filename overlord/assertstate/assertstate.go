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

	"github.com/ddkwork/golibrary/mylog"
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
	if errors.Is(err, &asserts.NotFoundError{}) {
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

	deviceCtx := mylog.Check2(snapstate.DevicePastSeeding(s, nil))

	snapStates := mylog.Check2(snapstate.All(s))
	mylog.Check(bulkRefreshSnapDeclarations(s, snapStates, userID, deviceCtx, opts))
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
			mylog.Check(snapasserts.FetchSnapDeclaration(f, sideInfo.SnapID))

		}

		// fetch store assertion if available
		if modelAs.Store() != "" {
			mylog.Check(snapasserts.FetchStore(f, modelAs.Store()))
			if err != nil && !errors.Is(err, &asserts.NotFoundError{}) {
				return err
			}
		}

		return nil
	}
	return doFetch(s, userID, deviceCtx, nil, fetching)
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
	snapStates := mylog.Check2(snapstate.All(s))

	for instanceName, snapst := range snapStates {
		info := mylog.Check2(snapst.CurrentInfo())

		if info.SnapID == "" {
			continue
		}
		gatingID := info.SnapID
		if gatingNames[gatingID] != "" {
			continue
		}
		a := mylog.Check2(db.Find(asserts.SnapDeclarationType, map[string]string{
			"series":  release.Series,
			"snap-id": gatingID,
		}))

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
				mylog.Check(f.Fetch(valref))
				if notFound, ok := err.(*asserts.NotFoundError); ok && notFound.Type == asserts.ValidationType {
					return fmt.Errorf("no validation by %q", gatingNames[gatingID])
				}

				validationRefs = append(validationRefs, valref)
			}
			return nil
		}
		mylog.Check(doFetch(s, userID, deviceCtx, nil, fetching))

		var revoked *asserts.Validation
		for _, valref := range validationRefs {
			a := mylog.Check2(valref.Resolve(db.Find))

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
	a := mylog.Check2(db.Find(asserts.SnapDeclarationType, map[string]string{
		"series":  release.Series,
		"snap-id": snapID,
	}))

	return a.(*asserts.SnapDeclaration), nil
}

// Publisher returns the account assertion for publisher of the given snap-id if it is present in the system assertion database.
func Publisher(s *state.State, snapID string) (*asserts.Account, error) {
	db := DB(s)
	a := mylog.Check2(db.Find(asserts.SnapDeclarationType, map[string]string{
		"series":  release.Series,
		"snap-id": snapID,
	}))

	snapDecl := a.(*asserts.SnapDeclaration)
	a = mylog.Check2(db.Find(asserts.AccountType, map[string]string{
		"account-id": snapDecl.PublisherID(),
	}))

	return a.(*asserts.Account), nil
}

// PublisherStoreAccount returns the store account information from the publisher assertion.
func PublisherStoreAccount(st *state.State, snapID string) (snap.StoreAccount, error) {
	if snapID == "" {
		return snap.StoreAccount{}, nil
	}

	pubAcct := mylog.Check2(Publisher(st, snapID))

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
	a := mylog.Check2(db.Find(asserts.StoreType, map[string]string{
		"store": store,
	}))

	return a.(*asserts.Store), nil
}

// AutoAliases returns the explicit automatic aliases alias=>app mapping for the given installed snap.
func AutoAliases(s *state.State, info *snap.Info) (map[string]string, error) {
	if info.SnapID == "" {
		// without declaration
		return nil, nil
	}
	decl := mylog.Check2(SnapDeclaration(s, info.SnapID))

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
	snapstate.EnforcedValidationSets = TrackedEnforcedValidationSets
	// hook the helper for saving current validation sets to the stack
	snapstate.AddCurrentTrackingToValidationSetsStack = addCurrentTrackingToValidationSetsHistory
	// hook the helper for restoring validation sets tracking from the stack
	snapstate.RestoreValidationSetsTracking = RestoreValidationSetsTracking
	// hook helper for enforcing validation sets without fetching them
	snapstate.EnforceValidationSets = ApplyEnforcedValidationSets
	// hook helper for enforcing already existing validation set assertions
	snapstate.EnforceLocalValidationSets = ApplyLocalEnforcedValidationSets
}

// AutoRefreshAssertions tries to refresh all assertions
func AutoRefreshAssertions(s *state.State, userID int) error {
	opts := &RefreshAssertionsOptions{IsAutoRefresh: true}
	mylog.Check(RefreshSnapDeclarations(s, userID, opts))

	return RefreshValidationSetAssertions(s, userID, opts)
}

// RefreshSnapAssertions tries to refresh all snap-centered assertions
func RefreshSnapAssertions(s *state.State, userID int, opts *RefreshAssertionsOptions) error {
	if opts == nil {
		opts = &RefreshAssertionsOptions{}
	}
	opts.IsAutoRefresh = false
	mylog.Check(RefreshSnapDeclarations(s, userID, opts))

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

	deviceCtx := mylog.Check2(snapstate.DevicePastSeeding(s, nil))

	vsets := mylog.Check2(ValidationSets(s))

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
				as := mylog.Check2(db.FindSequence(asserts.ValidationSetType, headers, -1, asserts.ValidationSetType.MaxSupportedFormat()))

				if vs.Current != as.Sequence() {
					vs.Current = as.Sequence()
					UpdateValidationSet(s, vs)
				}
			}
		}
		return nil
	}
	mylog.Check(bulkRefreshValidationSetAsserts(s, monitorModeSets, nil, userID, deviceCtx, opts))
	mylog.Check(updateTracking(monitorModeSets))

	checkConflictsAndPresence := func(db *asserts.Database, bs asserts.Backstore) error {
		vsets := snapasserts.NewValidationSets()
		tmpDb := db.WithStackedBackstore(bs)
		for _, vs := range enforceModeSets {
			headers := map[string]string{
				"series":     release.Series,
				"account-id": vs.AccountID,
				"name":       vs.Name,
			}

			var as asserts.Assertion
			if vs.PinnedAt > 0 {
				headers["sequence"] = fmt.Sprintf("%d", vs.PinnedAt)
				as = mylog.Check2(tmpDb.Find(asserts.ValidationSetType, headers))
			} else {
				as = mylog.Check2(tmpDb.FindSequence(asserts.ValidationSetType, headers, -1, asserts.ValidationSetType.MaxSupportedFormat()))
			}

			vsass, ok := as.(*asserts.ValidationSet)
			if !ok {
				return fmt.Errorf("internal error: unexpected assertion type %s for %s", as.Type().Name, ValidationSetKey(vs.AccountID, vs.Name))
			}
			mylog.Check(vsets.Add(vsass))

		}
		mylog.Check(vsets.Conflict())

		snaps, ignoreValidation := mylog.Check3(snapstate.InstalledSnaps(s))
		mylog.Check(vsets.CheckInstalledSnaps(snaps, ignoreValidation))
		if verr, ok := err.(*snapasserts.ValidationSetsValidationError); ok {
			if len(verr.InvalidSnaps) > 0 || len(verr.MissingSnaps) > 0 {
				return verr
			}
			// ignore wrong revisions
			return nil
		}
		return err
	}
	mylog.Check(bulkRefreshValidationSetAsserts(s, enforceModeSets, checkConflictsAndPresence, userID, deviceCtx, opts))
	mylog.Check(updateTracking(enforceModeSets))

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
	deviceCtx := mylog.Check2(snapstate.DevicePastSeeding(st, nil))

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
		vs = mylog.Check2(db.Find(asserts.ValidationSetType, headers))
	} else {
		// find latest
		vs = mylog.Check2(db.FindSequence(asserts.ValidationSetType, headers, -1, -1))
	}
	if err != nil && !errors.Is(err, &asserts.NotFoundError{}) {
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
		mylog.Check(pool.AddUnresolvedSequence(atSeq, atSeq.Unique()))
	} else {
		atSeq.Sequence = as.Sequence()
		// found locally, try to update
		atSeq.Revision = as.Revision()
		mylog.Check(pool.AddSequenceToUpdate(atSeq, atSeq.Unique()))

	}

	refreshOpts := &RefreshAssertionsOptions{IsAutoRefresh: false}
	mylog.Check(resolvePoolNoFallback(st, pool, nil, userID, deviceCtx, refreshOpts))

	// fallback: support the scenario of local assertion (snap ack)
	// not available in the store.

	// fetch the requested assertion again
	if pinned {
		vs = mylog.Check2(db.Find(asserts.ValidationSetType, headers))
	} else {
		vs = mylog.Check2(db.FindSequence(asserts.ValidationSetType, headers, -1, asserts.ValidationSetType.MaxSupportedFormat()))
	}
	if err == nil {
		as = vs.(*asserts.ValidationSet)
	}
	return as, false, err
}

func getSpecificSequenceOrLatest(db *asserts.Database, headers map[string]string) (vs *asserts.ValidationSet, err error) {
	var a asserts.Assertion
	if _, ok := headers["sequence"]; ok {
		a = mylog.Check2(db.Find(asserts.ValidationSetType, headers))
	} else {
		a = mylog.Check2(db.FindSequence(asserts.ValidationSetType, headers, -1, -1))
	}

	vs = a.(*asserts.ValidationSet)
	return vs, nil
}

// validationSetAssertionForEnforce tries to fetch the validation set assertion
// with the given accountID/name/sequence (sequence is optional) using pool and
// checks if it's not in conflict with existing validation sets in enforcing mode.
func validationSetAssertionForEnforce(st *state.State, accountID, name string, sequence int, userID int, snaps []*snapasserts.InstalledSnap, ignoreValidation map[string]bool) (vs *asserts.ValidationSet, err error) {
	deviceCtx := mylog.Check2(snapstate.DevicePastSeeding(st, nil))

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

	vs = mylog.Check2(getSpecificSequenceOrLatest(db, headers))

	checkForConflicts := func() error {
		valsets := mylog.Check2(TrackedEnforcedValidationSets(st, vs))
		mylog.Check(valsets.Conflict())
		mylog.Check(valsets.CheckInstalledSnaps(snaps, ignoreValidation))

		return nil
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
			return nil, trerr
		}
		// not tracked, update the assertion
		if errors.Is(trerr, state.ErrNoState) {
			// update with pool
			atSeq.Sequence = vs.Sequence()
			atSeq.Revision = vs.Revision()
			mylog.Check(pool.AddSequenceToUpdate(atSeq, atSeq.Unique()))

		} else {
			mylog.Check(
				// was already tracked, add to validation sets and check
				checkForConflicts())

			return vs, nil
		}
	} else {
		if !errors.Is(err, &asserts.NotFoundError{}) {
			return nil, err
		}
		mylog.Check(

			// try to resolve with pool
			pool.AddUnresolvedSequence(atSeq, atSeq.Unique()))

	}

	checkBeforeCommit := func(db *asserts.Database, bs asserts.Backstore) error {
		tmpDb := db.WithStackedBackstore(bs)
		// get the resolved validation set assert, add to validation sets and check
		vs = mylog.Check2(getSpecificSequenceOrLatest(tmpDb, headers))
		mylog.Check(checkForConflicts())

		// all fine, will be committed (along with its prerequisites if any) on
		// return by resolvePoolNoFallback
		return nil
	}

	opts := &RefreshAssertionsOptions{IsAutoRefresh: false}
	mylog.Check(resolvePoolNoFallback(st, pool, checkBeforeCommit, userID, deviceCtx, opts))

	return vs, err
}

// TryEnforcedValidationSets tries to fetch the given validation sets and
// enforce them (together with currently tracked validation sets) against
// installed snaps, but doesn't update tracking information in case of an error.
// It may return snapasserts.ValidationSetsValidationError which can be used to
// install/remove snaps as required to satisfy validation sets constraints.
func TryEnforcedValidationSets(st *state.State, validationSets []string, userID int, snaps []*snapasserts.InstalledSnap, ignoreValidation map[string]bool) error {
	deviceCtx := mylog.Check2(snapstate.DevicePastSeeding(st, nil))

	db := cachedDB(st)
	pool := asserts.NewPool(db, maxGroups)

	extraVsHeaders := make([]map[string]string, 0, len(validationSets))
	newTracking := make([]*ValidationSetTracking, 0, len(validationSets))

	for _, vsstr := range validationSets {
		accountID, name, sequence := mylog.Check4(snapasserts.ParseValidationSet(vsstr))

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

		vs := mylog.Check2(getSpecificSequenceOrLatest(db, headers))
		// found locally
		if err == nil {
			// update with pool
			atSeq.Sequence = vs.Sequence()
			atSeq.Revision = vs.Revision()
			mylog.Check(pool.AddSequenceToUpdate(atSeq, atSeq.Unique()))

		} else {
			if !errors.Is(err, &asserts.NotFoundError{}) {
				return err
			}
			mylog.Check(
				// try to resolve with pool
				pool.AddUnresolvedSequence(atSeq, atSeq.Unique()))

		}
	}

	checkBeforeCommit := func(db *asserts.Database, bs asserts.Backstore) error {
		tmpDb := db.WithStackedBackstore(bs)
		// get the resolved validation set asserts, add to validation sets and check
		var extraVs []*asserts.ValidationSet
		for _, headers := range extraVsHeaders {
			vs := mylog.Check2(getSpecificSequenceOrLatest(tmpDb, headers))

			extraVs = append(extraVs, vs)
		}

		valsets := mylog.Check2(TrackedEnforcedValidationSets(st, extraVs...))
		mylog.Check(valsets.Conflict())
		mylog.Check(valsets.CheckInstalledSnaps(snaps, ignoreValidation))
		// the returned error may be ValidationSetsValidationError which is normal and means we cannot enforce
		// the new validation sets - the caller should resolve the error and retry.

		// all fine, will be committed (along with its prerequisites if any) on
		// return by resolvePoolNoFallback
		return nil
	}

	opts := &RefreshAssertionsOptions{}
	mylog.Check(resolvePoolNoFallback(st, pool, checkBeforeCommit, userID, deviceCtx, opts))

	// no error, all validation-sets can be enforced, update tracking for all vsets
	for i, headers := range extraVsHeaders {
		tr := newTracking[i]

		if tr.PinnedAt == 0 {
			// if unpinned, get latest assertion from the db to determine current
			a := mylog.Check2(db.FindSequence(asserts.ValidationSetType, headers, -1, -1))

			// this is unexpected since all asserts should be resolved and committed at this point

			tr.Current = a.Sequence()
		} else {
			// no need to get latest since Current must be the same as pinned
			tr.Current = tr.PinnedAt
		}
	}
	for _, tr := range newTracking {
		UpdateValidationSet(st, tr)
	}

	return addCurrentTrackingToValidationSetsHistory(st)
}

func resolveValidationSetPrimaryKeys(st *state.State, vsKeys map[string][]string) (map[string]*asserts.ValidationSet, error) {
	db := cachedDB(st)
	valsets := make(map[string]*asserts.ValidationSet, len(vsKeys))
	for key, pk := range vsKeys {
		hdrs := mylog.Check2(asserts.HeadersFromPrimaryKey(asserts.ValidationSetType, pk))

		a := mylog.Check2(db.Find(asserts.ValidationSetType, hdrs))

		valsets[key] = a.(*asserts.ValidationSet)
	}
	return valsets, nil
}

func validationSetTrackings(valsets map[string]*asserts.ValidationSet, pinnedSeqs map[string]int) ([]*asserts.ValidationSet, []*ValidationSetTracking, error) {
	valsetsSlice := make([]*asserts.ValidationSet, 0, len(valsets))
	valsetsTracking := make([]*ValidationSetTracking, 0, len(valsets))

	for vsKey, vs := range valsets {
		pinnedSeq := pinnedSeqs[vsKey]
		if pinnedSeq != 0 && pinnedSeq != vs.Sequence() {
			// shouldn't be possible save for programmer error since, if we have a pinned
			// sequence here, it should've been used when fetching the assertion
			return nil, nil, fmt.Errorf("internal error: trying to enforce validation set %q with sequence point %d different than pinned %d",
				vsKey, vs.Sequence(), pinnedSeq)
		}

		tr := &ValidationSetTracking{
			AccountID: vs.AccountID(),
			Name:      vs.Name(),
			Mode:      Enforce,
			Current:   vs.Sequence(),
			// may be 0 meaning no pinning
			PinnedAt: pinnedSeq,
		}

		valsetsTracking = append(valsetsTracking, tr)
		valsetsSlice = append(valsetsSlice, vs)
	}
	return valsetsSlice, valsetsTracking, nil
}

// ApplyLocalEnforcedValidationSets enforces the supplied validation sets. It takes a map
// of validation set keys to validation sets, pinned sequence numbers (if any),
// installed snaps and ignored snaps. The local in this naming indicates that it uses the
// validation-set primary keys to lookup assertions in the current database. No fetching is
// done contrary to the non-local version.
func ApplyLocalEnforcedValidationSets(st *state.State, vsKeys map[string][]string, pinnedSeqs map[string]int, snaps []*snapasserts.InstalledSnap, ignoreValidation map[string]bool) error {
	valsets := mylog.Check2(resolveValidationSetPrimaryKeys(st, vsKeys))

	valsetsSlice, valsetsTracking := mylog.Check3(validationSetTrackings(valsets, pinnedSeqs))

	valsetGroup := mylog.Check2(TrackedEnforcedValidationSets(st, valsetsSlice...))
	mylog.Check(valsetGroup.Conflict())
	mylog.Check(valsetGroup.CheckInstalledSnaps(snaps, ignoreValidation))

	for _, tr := range valsetsTracking {
		UpdateValidationSet(st, tr)
	}

	return addCurrentTrackingToValidationSetsHistory(st)
}

// ApplyEnforcedValidationSets enforces the supplied validation sets. It takes a map
// of validation set keys to validation sets, pinned sequence numbers (if any),
// installed snaps and ignored snaps. It fetches any pre-requisites necessary.
func ApplyEnforcedValidationSets(st *state.State, valsets map[string]*asserts.ValidationSet, pinnedSeqs map[string]int, snaps []*snapasserts.InstalledSnap, ignoreValidation map[string]bool, userID int) error {
	deviceCtx := mylog.Check2(snapstate.DevicePastSeeding(st, nil))

	db := cachedDB(st)
	batch := asserts.NewBatch(handleUnsupported(db))

	valsetsSlice, valsetsTracking := mylog.Check3(validationSetTrackings(valsets, pinnedSeqs))
	mylog.Check(doFetch(st, userID, deviceCtx, batch, func(f asserts.Fetcher) error {
		for vsKey, vs := range valsets {
			mylog.Check(f.Save(vs))
		}
		return nil
	}))

	valsetGroup := mylog.Check2(TrackedEnforcedValidationSets(st, valsetsSlice...))
	mylog.Check(valsetGroup.Conflict())
	mylog.Check(valsetGroup.CheckInstalledSnaps(snaps, ignoreValidation))
	mylog.Check(batch.CommitTo(db, nil))

	for _, tr := range valsetsTracking {
		UpdateValidationSet(st, tr)
	}

	return addCurrentTrackingToValidationSetsHistory(st)
}

func validationSetFromModel(st *state.State, accountID, name string) (*asserts.ModelValidationSet, error) {
	deviceCtx := mylog.Check2(snapstate.DevicePastSeeding(st, nil))

	model := deviceCtx.Model()
	for _, vs := range model.ValidationSets() {
		if vs.AccountID == accountID && vs.Name == name {
			return vs, nil
		}
	}
	return nil, nil
}

func sequenceSetByModelAssertion(st *state.State, accountID, name string) (int, error) {
	vs := mylog.Check2(validationSetFromModel(st, accountID, name))

	if vs == nil {
		return 0, nil
	}
	return vs.Sequence, nil
}

func validateSequenceAgainstModel(st *state.State, accountID, name string, sequence int) (int, error) {
	modelSeq := mylog.Check2(sequenceSetByModelAssertion(st, accountID, name))

	// Verify the sequence requested does not differ from the one specified by the model
	// in case one is set.
	if sequence > 0 {
		// Sequence was set, it must match any requirements set by model.
		if modelSeq > 0 && modelSeq != sequence {
			return 0, fmt.Errorf("only sequence %d allowed by model", modelSeq)
		}
	} else if modelSeq > 0 {
		// Sequence was set by model, use that specifically.
		sequence = modelSeq
	}
	return sequence, nil
}

// FetchAndApplyEnforcedValidationSet tries to fetch the given validation set and enforce it.
// If all validation sets constrains are satisfied, the current validation sets
// tracking state is saved in validation sets history.
func FetchAndApplyEnforcedValidationSet(st *state.State, accountID, name string, sequence, userID int, snaps []*snapasserts.InstalledSnap, ignoreValidation map[string]bool) (*ValidationSetTracking, error) {
	// If the model has a specific sequence specified, then either we may
	// need to use the correct sequence (if no specific is requested)
	// or we may need to throw a validation error if the user is requesting a
	// different sequence than is allowed by the model.
	modelSeq := mylog.Check2(validateSequenceAgainstModel(st, accountID, name, sequence))

	vs := mylog.Check2(validationSetAssertionForEnforce(st, accountID, name, modelSeq, userID, snaps, ignoreValidation))

	tr := ValidationSetTracking{
		AccountID: accountID,
		Name:      name,
		Mode:      Enforce,
		// note, modelSeq may be 0, meaning not pinned.
		PinnedAt: modelSeq,
		Current:  vs.Sequence(),
	}

	UpdateValidationSet(st, &tr)
	mylog.Check(addCurrentTrackingToValidationSetsHistory(st))
	return &tr, err
}

// MonitorValidationSet tries to fetch the given validation set and monitor it.
// The current validation sets tracking state is saved in validation sets history.
func MonitorValidationSet(st *state.State, accountID, name string, sequence int, userID int) (*ValidationSetTracking, error) {
	// If the model has a specific sequence specified, then either we may
	// need to use the correct sequence (if no specific is requested)
	// or we may need to throw a validation error if the user is requesting a
	// different sequence than is allowed by the model.
	modelSeq := mylog.Check2(validateSequenceAgainstModel(st, accountID, name, sequence))

	pinned := modelSeq > 0
	opts := ResolveOptions{AllowLocalFallback: true}
	as, local := mylog.Check3(validationSetAssertionForMonitor(st, accountID, name, modelSeq, pinned, userID, &opts))

	tr := &ValidationSetTracking{
		AccountID: accountID,
		Name:      name,
		Mode:      Monitor,
		// note, modelSeq may be 0, meaning not pinned.
		PinnedAt:  modelSeq,
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

// FetchValidationSetsOptions contains options for FetchValidationSets.
type FetchValidationSetsOptions struct {
	// Offline should be set to true if the store should not be accessed. Any
	// assertions will be retrieved from the existing assertions database. If
	// the assertions are not present in the database, an error will be
	// returned.
	Offline bool
}

// FetchValidationSets fetches the given validation set assertions from either
// the store or the existing assertions database. The validation sets are added
// to a snapasserts.ValidationSets, checked for any conflicts, and returned.
func FetchValidationSets(st *state.State, toFetch []*asserts.AtSequence, opts FetchValidationSetsOptions, deviceCtx snapstate.DeviceContext) (*snapasserts.ValidationSets, error) {
	var sets []*asserts.ValidationSet
	save := func(a asserts.Assertion) error {
		if vs, ok := a.(*asserts.ValidationSet); ok {
			sets = append(sets, vs)
		}
		mylog.Check(Add(st, a))

		return nil
	}

	db := DB(st)

	store := snapstate.Store(st, deviceCtx)

	retrieve := func(ref *asserts.Ref) (asserts.Assertion, error) {
		if opts.Offline {
			return ref.Resolve(db.Find)
		}

		st.Unlock()
		defer st.Lock()

		return store.Assertion(ref.Type, ref.PrimaryKey, nil)
	}

	retrieveSeq := func(ref *asserts.AtSequence) (asserts.Assertion, error) {
		if opts.Offline {
			return resolveValidationSetAssertion(ref, db)
		}

		st.Unlock()
		defer st.Lock()

		return store.SeqFormingAssertion(ref.Type, ref.SequenceKey, ref.Sequence, nil)
	}

	fetcher := asserts.NewSequenceFormingFetcher(db, retrieve, retrieveSeq, save)

	for _, vs := range toFetch {
		mylog.Check(fetcher.FetchSequence(vs))
	}

	vSets := snapasserts.NewValidationSets()
	for _, vs := range sets {
		vSets.Add(vs)
	}
	mylog.Check(vSets.Conflict())

	return vSets, nil
}

// ValidationSetsFromModel takes in a model and creates a
// snapasserts.ValidationSets from any validation sets that the model includes.
func ValidationSetsFromModel(st *state.State, model *asserts.Model, opts FetchValidationSetsOptions, deviceCtx snapstate.DeviceContext) (*snapasserts.ValidationSets, error) {
	toFetch := make([]*asserts.AtSequence, 0, len(model.ValidationSets()))
	for _, vs := range model.ValidationSets() {
		toFetch = append(toFetch, vs.AtSequence())
	}

	return FetchValidationSets(st, toFetch, opts, deviceCtx)
}

func resolveValidationSetAssertion(seq *asserts.AtSequence, db asserts.RODatabase) (asserts.Assertion, error) {
	if seq.Sequence <= 0 {
		hdrs := mylog.Check2(asserts.HeadersFromSequenceKey(seq.Type, seq.SequenceKey))

		return db.FindSequence(seq.Type, hdrs, -1, seq.Type.MaxSupportedFormat())
	}
	return seq.Resolve(db.Find)
}

// AspectBundle returns the aspect-bundle for the given account and bundle name,
// if it's present in the system assertion database.
func AspectBundle(s *state.State, account, bundleName string) (*asserts.AspectBundle, error) {
	db := DB(s)
	as := mylog.Check2(db.Find(asserts.AspectBundleType, map[string]string{
		"account-id": account,
		"name":       bundleName,
	}))

	return as.(*asserts.AspectBundle), nil
}
