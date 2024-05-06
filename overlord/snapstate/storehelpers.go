// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2016-2018 Canonical Ltd
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

package snapstate

import (
	"context"
	"errors"
	"fmt"
	"sort"

	"github.com/snapcore/snapd/asserts"
	"github.com/snapcore/snapd/asserts/snapasserts"
	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/overlord/auth"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/snap/naming"
	"github.com/snapcore/snapd/store"
	"github.com/snapcore/snapd/strutil"
)

var currentSnaps = currentSnapsImpl

// EnforcedValidationSets allows to hook getting of validation sets in enforce
// mode into installation/refresh/removal of snaps. It gets hooked from
// assertstate.
var EnforcedValidationSets func(st *state.State, extraVss ...*asserts.ValidationSet) (*snapasserts.ValidationSets, error)

// EnforceLocalValidationSets allows to hook enforcing validation sets without
// fetching them or their dependencies. It's hooked from assertstate.
var EnforceLocalValidationSets func(*state.State, map[string][]string, map[string]int, []*snapasserts.InstalledSnap, map[string]bool) error

// EnforceValidationSets allows to hook enforcing validation sets without
// fetching them. It's hooked from assertstate.
var EnforceValidationSets func(*state.State, map[string]*asserts.ValidationSet, map[string]int, []*snapasserts.InstalledSnap, map[string]bool, int) error

func userIDForSnap(st *state.State, snapst *SnapState, fallbackUserID int) (int, error) {
	userID := snapst.UserID
	_, err := auth.User(st, userID)
	if err == nil {
		return userID, nil
	}
	if err != auth.ErrInvalidUser {
		return 0, err
	}
	return fallbackUserID, nil
}

// userFromUserID returns the first valid user from a series of userIDs
// used as successive fallbacks.
func userFromUserID(st *state.State, userIDs ...int) (*auth.UserState, error) {
	var user *auth.UserState
	var err error
	for _, userID := range userIDs {
		if userID == 0 {
			err = nil
			continue
		}
		user, err = auth.User(st, userID)
		if err != auth.ErrInvalidUser {
			break
		}
	}
	return user, err
}

func refreshOptions(st *state.State, origOpts *store.RefreshOptions) (*store.RefreshOptions, error) {
	var opts store.RefreshOptions

	if origOpts != nil {
		if origOpts.PrivacyKey != "" {
			// nothing to add
			return origOpts, nil
		}
		opts = *origOpts
	}

	if err := st.Get("refresh-privacy-key", &opts.PrivacyKey); err != nil && !errors.Is(err, state.ErrNoState) {
		return nil, fmt.Errorf("cannot obtain store request salt: %v", err)
	}
	if opts.PrivacyKey == "" {
		return nil, fmt.Errorf("internal error: request salt is unset")
	}
	return &opts, nil
}

// installSize returns total download size of snaps and their prerequisites
// (bases and default content providers), querying the store as necessary,
// potentially more than once. It assumes the initial list of snaps already has
// download infos set.
// The state must be locked by the caller.
var installSize = func(st *state.State, snaps []minimalInstallInfo, userID int, prqt PrereqTracker) (uint64, error) {
	curSnaps, err := currentSnaps(st)
	if err != nil {
		return 0, err
	}

	user, err := userFromUserID(st, userID)
	if err != nil {
		return 0, err
	}

	accountedSnaps := map[string]bool{}
	for _, snap := range curSnaps {
		accountedSnaps[snap.InstanceName] = true
	}

	// if the prerequisites are included in the install, don't query the store
	// for info on them
	for _, snap := range snaps {
		accountedSnaps[snap.InstanceName()] = true
	}

	var prereqs []string

	resolveBaseAndContentProviders := func(inst minimalInstallInfo) {
		if inst.Type() != snap.TypeApp {
			return
		}
		if inst.SnapBase() != "none" {
			base := defaultCoreSnapName
			if inst.SnapBase() != "" {
				base = inst.SnapBase()
			}
			if !accountedSnaps[base] {
				prereqs = append(prereqs, base)
				accountedSnaps[base] = true
			}
		}
		for _, snapName := range inst.Prereq(st, prqt) {
			if !accountedSnaps[snapName] {
				prereqs = append(prereqs, snapName)
				accountedSnaps[snapName] = true
			}
		}
	}

	snapSizes := map[string]uint64{}
	for _, inst := range snaps {
		if inst.DownloadSize() == 0 {
			return 0, fmt.Errorf("internal error: download info missing for %q", inst.InstanceName())
		}
		snapSizes[inst.InstanceName()] = uint64(inst.DownloadSize())
		resolveBaseAndContentProviders(inst)
	}

	opts, err := refreshOptions(st, nil)
	if err != nil {
		return 0, err
	}

	theStore := Store(st, nil)
	channel := defaultPrereqSnapsChannel()

	// this can potentially be executed multiple times if we (recursively)
	// find new prerequisites or bases.
	for len(prereqs) > 0 {
		actions := []*store.SnapAction{}
		for _, prereq := range prereqs {
			action := &store.SnapAction{
				Action:       "install",
				InstanceName: prereq,
				Channel:      channel,
			}
			actions = append(actions, action)
		}

		// calls to the store should be done without holding the state lock
		st.Unlock()
		results, _, err := theStore.SnapAction(context.TODO(), curSnaps, actions, nil, user, opts)
		st.Lock()
		if err != nil {
			return 0, err
		}
		prereqs = []string{}
		for _, res := range results {
			snapSizes[res.InstanceName()] = uint64(res.Size)
			// results may have new base or content providers
			resolveBaseAndContentProviders(installSnapInfo{res.Info})
		}
	}

	// state is locked at this point

	// since we unlock state above when querying store, other changes may affect
	// same snaps, therefore obtain current snaps again and only compute total
	// size of snaps that would actually need to be installed.
	curSnaps, err = currentSnaps(st)
	if err != nil {
		return 0, err
	}
	for _, snap := range curSnaps {
		delete(snapSizes, snap.InstanceName)
	}

	var total uint64
	for _, sz := range snapSizes {
		total += sz
	}

	return total, nil
}

func setActionValidationSetsAndRequiredRevision(action *store.SnapAction, valsets []snapasserts.ValidationSetKey, requiredRevision snap.Revision) {
	for _, vs := range valsets {
		action.ValidationSets = append(action.ValidationSets, vs)
	}
	if !requiredRevision.Unset() {
		action.Revision = requiredRevision
		// channel cannot be present if revision is set (store would
		// respond with revision-conflict error).
		action.Channel = ""
	}
}

func downloadInfo(ctx context.Context, st *state.State, name string, revOpts *RevisionOptions, userID int, deviceCtx DeviceContext) (store.SnapActionResult, error) {
	curSnaps, err := currentSnaps(st)
	if err != nil {
		return store.SnapActionResult{}, err
	}

	user, err := userFromUserID(st, userID)
	if err != nil {
		return store.SnapActionResult{}, err
	}

	opts, err := refreshOptions(st, nil)
	if err != nil {
		return store.SnapActionResult{}, err
	}

	action := &store.SnapAction{
		Action:       "download",
		InstanceName: name,
	}

	if revOpts != nil {
		// cannot specify both with the API
		if revOpts.Revision.Unset() {
			action.Channel = revOpts.Channel
			action.CohortKey = revOpts.CohortKey
		} else {
			action.Revision = revOpts.Revision
		}
	}

	theStore := Store(st, deviceCtx)
	st.Unlock() // calls to the store should be done without holding the state lock
	res, _, err := theStore.SnapAction(ctx, curSnaps, []*store.SnapAction{action}, nil, user, opts)
	st.Lock()

	return singleActionResult(name, action.Action, res, err)
}

func installInfo(ctx context.Context, st *state.State, name string, revOpts *RevisionOptions, userID int, flags Flags, deviceCtx DeviceContext) (store.SnapActionResult, error) {
	curSnaps, err := currentSnaps(st)
	if err != nil {
		return store.SnapActionResult{}, err
	}

	user, err := userFromUserID(st, userID)
	if err != nil {
		return store.SnapActionResult{}, err
	}

	opts, err := refreshOptions(st, nil)
	if err != nil {
		return store.SnapActionResult{}, err
	}

	action := &store.SnapAction{
		Action:       "install",
		InstanceName: name,
	}

	if flags.IgnoreValidation {
		action.Flags = store.SnapActionIgnoreValidation
	}

	var requiredRevision snap.Revision
	var requiredValSets []snapasserts.ValidationSetKey

	if !flags.IgnoreValidation {
		if len(revOpts.ValidationSets) > 0 {
			requiredRevision = revOpts.Revision
			requiredValSets = revOpts.ValidationSets
		} else {
			enforcedSets, err := EnforcedValidationSets(st)
			if err != nil {
				return store.SnapActionResult{}, err
			}

			if enforcedSets != nil {
				// check for invalid presence first to have a list of sets where it's invalid
				invalidForValSets, err := enforcedSets.CheckPresenceInvalid(naming.Snap(name))
				if err != nil {
					if _, ok := err.(*snapasserts.PresenceConstraintError); !ok {
						return store.SnapActionResult{}, err
					} // else presence is optional or required, carry on
				}
				if len(invalidForValSets) > 0 {
					return store.SnapActionResult{}, fmt.Errorf("cannot install snap %q due to enforcing rules of validation set %s", name, snapasserts.ValidationSetKeySlice(invalidForValSets).CommaSeparated())
				}
				requiredValSets, requiredRevision, err = enforcedSets.CheckPresenceRequired(naming.Snap(name))
				if err != nil {
					return store.SnapActionResult{}, err
				}
			}

			// check if desired revision matches the revision required by validation sets
			if !requiredRevision.Unset() && !revOpts.Revision.Unset() && revOpts.Revision.N != requiredRevision.N {
				return store.SnapActionResult{}, fmt.Errorf("cannot install snap %q at requested revision %s without --ignore-validation, revision %s required by validation sets: %s",
					name, revOpts.Revision, requiredRevision, snapasserts.ValidationSetKeySlice(requiredValSets).CommaSeparated())
			}
		}
	}

	if len(requiredValSets) > 0 {
		setActionValidationSetsAndRequiredRevision(action, requiredValSets, requiredRevision)
	}

	if requiredRevision.Unset() {
		// cannot specify both with the API
		if revOpts.Revision.Unset() {
			// the desired channel
			action.Channel = revOpts.Channel
			// the desired cohort key
			action.CohortKey = revOpts.CohortKey
		} else {
			action.Revision = revOpts.Revision
		}
	}

	theStore := Store(st, deviceCtx)
	st.Unlock() // calls to the store should be done without holding the state lock
	res, _, err := theStore.SnapAction(ctx, curSnaps, []*store.SnapAction{action}, nil, user, opts)
	st.Lock()

	return singleActionResult(name, action.Action, res, err)
}

func updateInfo(st *state.State, snapst *SnapState, opts *RevisionOptions, userID int, flags Flags, deviceCtx DeviceContext) (*snap.Info, error) {
	curSnaps, err := currentSnaps(st)
	if err != nil {
		return nil, err
	}

	refreshOpts, err := refreshOptions(st, nil)
	if err != nil {
		return nil, err
	}

	curInfo, user, err := preUpdateInfo(st, snapst, flags.Amend, userID)
	if err != nil {
		return nil, err
	}

	var storeFlags store.SnapActionFlags
	if flags.IgnoreValidation {
		storeFlags = store.SnapActionIgnoreValidation
	} else {
		storeFlags = store.SnapActionEnforceValidation
	}

	action := &store.SnapAction{
		Action:       "refresh",
		InstanceName: curInfo.InstanceName(),
		SnapID:       curInfo.SnapID,
		// the desired channel
		Channel: opts.Channel,
		Flags:   storeFlags,
	}

	if len(opts.ValidationSets) > 0 {
		// update to a specific revision is handled by updateToRevisionInfo.
		// updating without a revision while enforcing validation sets is not a
		// viable scenario (although we could handle it if desired), we only install/refresh
		// what's missing and explicitly required by requested validation sets.
		return nil, fmt.Errorf("internal error: list of validation sets is not expected for update without revision")
	}

	var requiredRevision snap.Revision
	var requiredValsets []snapasserts.ValidationSetKey

	if !flags.IgnoreValidation {
		enforcedSets, err := EnforcedValidationSets(st)
		if err != nil {
			return nil, err
		}
		if enforcedSets != nil {
			requiredValsets, requiredRevision, err = enforcedSets.CheckPresenceRequired(naming.Snap(curInfo.InstanceName()))
			if err != nil {
				return nil, err
			}
			if !requiredRevision.Unset() && snapst.Current == requiredRevision {
				logger.Debugf("snap %q is already at the revision %s required by validation sets: %s, skipping",
					curInfo.InstanceName(), snapst.Current, snapasserts.ValidationSetKeySlice(requiredValsets).CommaSeparated())
				return nil, store.ErrNoUpdateAvailable
			}
			if len(requiredValsets) > 0 {
				setActionValidationSetsAndRequiredRevision(action, requiredValsets, requiredRevision)
			}
		}
	}

	// only set cohort if validation sets don't require a specific revision
	if action.Revision.Unset() {
		action.CohortKey = opts.CohortKey
	} else {
		// specific revision is required, reset cohort in current snaps
		for _, sn := range curSnaps {
			if sn.InstanceName == curInfo.InstanceName() {
				sn.CohortKey = ""
				break
			}
		}
	}

	if curInfo.SnapID == "" { // amend
		action.Action = "install"
		action.Epoch = curInfo.Epoch
	}

	theStore := Store(st, deviceCtx)
	st.Unlock() // calls to the store should be done without holding the state lock
	res, _, err := theStore.SnapAction(context.TODO(), curSnaps, []*store.SnapAction{action}, nil, user, refreshOpts)
	st.Lock()

	sar, err := singleActionResult(curInfo.InstanceName(), action.Action, res, err)
	return sar.Info, err
}

func preUpdateInfo(st *state.State, snapst *SnapState, amend bool, userID int) (*snap.Info, *auth.UserState, error) {
	user, err := userFromUserID(st, snapst.UserID, userID)
	if err != nil {
		return nil, nil, err
	}

	curInfo, err := snapst.CurrentInfo()
	if err != nil {
		return nil, nil, err
	}

	if curInfo.SnapID == "" { // covers also trymode
		if !amend {
			return nil, nil, store.ErrLocalSnap
		}
	}

	return curInfo, user, nil
}

var ErrMissingExpectedResult = fmt.Errorf("unexpectedly empty response from the server (try again later)")

func singleActionResult(name, action string, results []store.SnapActionResult, e error) (store.SnapActionResult, error) {
	if len(results) > 1 {
		return store.SnapActionResult{}, fmt.Errorf("internal error: multiple store results for a single snap op")
	}
	if len(results) > 0 {
		// TODO: if we also have an error log/warn about it
		return results[0], nil
	}

	if saErr, ok := e.(*store.SnapActionError); ok {
		if len(saErr.Other) != 0 {
			return store.SnapActionResult{}, saErr
		}

		var snapErr error
		switch action {
		case "refresh":
			snapErr = saErr.Refresh[name]
		case "download":
			snapErr = saErr.Download[name]
		case "install":
			snapErr = saErr.Install[name]
		}
		if snapErr != nil {
			return store.SnapActionResult{}, snapErr
		}

		// no result, atypical case
		if saErr.NoResults {
			return store.SnapActionResult{}, ErrMissingExpectedResult
		}
	}

	return store.SnapActionResult{}, e
}

func updateToRevisionInfo(st *state.State, snapst *SnapState, revOpts *RevisionOptions, userID int, flags Flags, deviceCtx DeviceContext) (*snap.Info, error) {
	curSnaps, err := currentSnaps(st)
	if err != nil {
		return nil, err
	}

	curInfo, user, err := preUpdateInfo(st, snapst, false, userID)
	if err != nil {
		return nil, err
	}

	opts, err := refreshOptions(st, nil)
	if err != nil {
		return nil, err
	}

	action := &store.SnapAction{
		Action:       "refresh",
		SnapID:       curInfo.SnapID,
		InstanceName: curInfo.InstanceName(),
		// the desired revision
		Revision: revOpts.Revision,
	}

	var requiredRevision snap.Revision
	var requiredValsets []snapasserts.ValidationSetKey

	var storeFlags store.SnapActionFlags
	if !flags.IgnoreValidation {
		if len(revOpts.ValidationSets) > 0 {
			requiredRevision = revOpts.Revision
			requiredValsets = revOpts.ValidationSets
		} else {
			enforcedSets, err := EnforcedValidationSets(st)
			if err != nil {
				return nil, err
			}
			if enforcedSets != nil {
				requiredValsets, requiredRevision, err = enforcedSets.CheckPresenceRequired(naming.Snap(curInfo.InstanceName()))
				if err != nil {
					return nil, err
				}
				if !requiredRevision.Unset() {
					if revOpts.Revision != requiredRevision {
						return nil, fmt.Errorf("cannot update snap %q to revision %s without --ignore-validation, revision %s is required by validation sets: %s",
							curInfo.InstanceName(), revOpts.Revision, requiredRevision, snapasserts.ValidationSetKeySlice(requiredValsets).CommaSeparated())
					}
					// note, not checking if required revision matches snapst.Current because
					// this is already indirectly prevented by infoForUpdate().

					// specific revision is required, reset cohort in current snaps
					for _, sn := range curSnaps {
						if sn.InstanceName == curInfo.InstanceName() {
							sn.CohortKey = ""
							break
						}
					}
				}
			}
		}
	} else {
		storeFlags = store.SnapActionIgnoreValidation
	}

	if len(requiredValsets) > 0 {
		setActionValidationSetsAndRequiredRevision(action, requiredValsets, requiredRevision)
	}

	action.Flags = storeFlags

	theStore := Store(st, deviceCtx)
	st.Unlock() // calls to the store should be done without holding the state lock
	res, _, err := theStore.SnapAction(context.TODO(), curSnaps, []*store.SnapAction{action}, nil, user, opts)
	st.Lock()

	sar, err := singleActionResult(curInfo.InstanceName(), action.Action, res, err)
	return sar.Info, err
}

func currentSnapsImpl(st *state.State) ([]*store.CurrentSnap, error) {
	snapStates, err := All(st)
	if err != nil {
		return nil, err
	}

	if len(snapStates) == 0 {
		// no snaps installed, do not bother any further
		return nil, nil
	}

	var names []string
	for _, snapst := range snapStates {
		names = append(names, snapst.InstanceName())
	}

	holds, err := SnapHolds(st, names)
	if err != nil {
		return nil, err
	}

	return collectCurrentSnaps(snapStates, holds, nil)
}

func collectCurrentSnaps(snapStates map[string]*SnapState, holds map[string][]string, consider func(*store.CurrentSnap, *SnapState) error) (curSnaps []*store.CurrentSnap, err error) {
	curSnaps = make([]*store.CurrentSnap, 0, len(snapStates))

	for _, snapst := range snapStates {
		if snapst.TryMode {
			// try mode snaps are completely local and
			// irrelevant for the operation
			continue
		}

		snapInfo, err := snapst.CurrentInfo()
		if err != nil {
			continue
		}

		if snapInfo.SnapID == "" {
			// the store won't be able to tell what this
			// is and so cannot include it in the
			// operation
			continue
		}

		installed := &store.CurrentSnap{
			InstanceName: snapInfo.InstanceName(),
			SnapID:       snapInfo.SnapID,
			// the desired channel (not snapInfo.Channel!)
			TrackingChannel:  snapst.TrackingChannel,
			Revision:         snapInfo.Revision,
			RefreshedDate:    revisionDate(snapInfo),
			IgnoreValidation: snapst.IgnoreValidation,
			Epoch:            snapInfo.Epoch,
			CohortKey:        snapst.CohortKey,
			HeldBy:           holds[snapInfo.InstanceName()],
		}
		curSnaps = append(curSnaps, installed)

		if consider != nil {
			if err := consider(installed, snapst); err != nil {
				return nil, err
			}
		}
	}

	return curSnaps, nil
}

// refreshCandidates is a wrapper for refreshCandidatesCore.
//
// It addresses the case where the store doesn't return refresh candidates for
// snaps with already existing monitored refresh-candidates due to inconsistent
// store return being caused by the throttling.
// A second request is sent for eligible snaps that might have been throttled
// with the RevisionOptions.Scheduled option turned off.
//
// Note: This wrapper is a short term solution and should be removed once a better
// solution is reached.
func refreshCandidates(ctx context.Context, st *state.State, names []string, revOpts []*RevisionOptions, user *auth.UserState, opts *store.RefreshOptions) ([]*snap.Info, map[string]*SnapState, map[string]bool, error) {
	// initialize options before using
	opts, err := refreshOptions(st, opts)
	if err != nil {
		return nil, nil, nil, err
	}

	var revOptsByName map[string]*RevisionOptions
	if revOpts != nil {
		revOptsByName = make(map[string]*RevisionOptions, len(revOpts))
		for i, opts := range revOpts {
			revOptsByName[names[i]] = opts
		}
	}

	updates, stateByInstanceName, ignoreValidation, err := refreshCandidatesCore(ctx, st, names, revOpts, user, opts)
	if err != nil {
		return nil, nil, nil, err
	}

	if !opts.Scheduled {
		// not an auto-refresh, just return what we got
		return updates, stateByInstanceName, ignoreValidation, nil
	}

	var oldHints map[string]*refreshCandidate
	if err := st.Get("refresh-candidates", &oldHints); err != nil {
		if errors.Is(err, &state.NoStateError{}) {
			// do nothing
			return updates, stateByInstanceName, ignoreValidation, nil
		}

		return nil, nil, nil, fmt.Errorf("cannot get refresh-candidates: %v", err)
	}

	var missingNames []string

	for name, hint := range oldHints {
		if stateByInstanceName[name] == nil {
			continue
		}
		if !hint.Monitored {
			continue
		}
		hasUpdate := false
		for _, update := range updates {
			if update.InstanceName() == name {
				hasUpdate = true
				break
			}
		}
		if hasUpdate {
			continue
		}

		missingNames = append(missingNames, name)
	}

	if len(missingNames) > 0 {
		var missingRevOpts []*RevisionOptions
		if revOpts != nil {
			for _, name := range missingNames {
				missingRevOpts = append(missingRevOpts, revOptsByName[name])
			}
		}
		// mimic manual refresh to avoid throttling.
		// context: snaps may be throttled by the store to balance load
		// and therefore may not always receive an update (even if one was
		// returned before). forcing a manual refresh should be fine since
		// we already started a pre-download for this snap, so no extra
		// load is being exerted on the store.
		opts.Scheduled = false
		moreUpdates, _, _, err := refreshCandidatesCore(ctx, st, missingNames, missingRevOpts, user, opts)
		if err != nil {
			return nil, nil, nil, err
		}
		updates = append(updates, moreUpdates...)
	}

	return updates, stateByInstanceName, ignoreValidation, nil
}

func refreshCandidatesCore(ctx context.Context, st *state.State, names []string, revOpts []*RevisionOptions, user *auth.UserState, opts *store.RefreshOptions) ([]*snap.Info, map[string]*SnapState, map[string]bool, error) {
	if opts == nil {
		return nil, nil, nil, fmt.Errorf("internal error: opts cannot be nil")
	}

	snapStates, err := All(st)
	if err != nil {
		return nil, nil, nil, err
	}

	// check if we have this name at all
	for _, name := range names {
		if _, ok := snapStates[name]; !ok {
			return nil, nil, nil, snap.NotInstalledError{Snap: name}
		}
	}

	var fallbackID int
	// normalize fallback user
	if !user.HasStoreAuth() {
		user = nil
	} else {
		fallbackID = user.ID
	}

	actionsByUserID := make(map[int][]*store.SnapAction)
	stateByInstanceName := make(map[string]*SnapState, len(snapStates))
	ignoreValidationByInstanceName := make(map[string]bool)
	nCands := 0

	var enforcedSets *snapasserts.ValidationSets
	var revOptsByName map[string]*RevisionOptions

	// if refreshing to specific revision to enforce a new validation set, we've
	// already checked against other enforced sets
	if revOpts == nil {
		enforcedSets, err = EnforcedValidationSets(st)
		if err != nil {
			return nil, nil, nil, err
		}
	} else {
		revOptsByName = make(map[string]*RevisionOptions, len(revOpts))
		for i, opts := range revOpts {
			revOptsByName[names[i]] = opts
		}
	}

	// sorting MUST be done after revOptsByName is built to avoid misalignment
	sort.Strings(names)

	addCand := func(installed *store.CurrentSnap, snapst *SnapState) error {
		// FIXME: snaps that are not active are skipped for now
		//        until we know what we want to do
		if !snapst.Active {
			return nil
		}

		if len(names) == 0 && snapst.DevMode {
			// no auto-refresh for devmode
			return nil
		}

		if len(names) > 0 && !strutil.SortedListContains(names, installed.InstanceName) {
			return nil
		}

		action := &store.SnapAction{
			Action:       "refresh",
			SnapID:       installed.SnapID,
			InstanceName: installed.InstanceName,
		}

		if !snapst.IgnoreValidation {
			var requiredValsets []snapasserts.ValidationSetKey
			var requiredRevision snap.Revision

			if revOpts != nil {
				opts := revOptsByName[installed.InstanceName]
				requiredValsets, requiredRevision = opts.ValidationSets, opts.Revision
			} else if enforcedSets != nil {
				requiredValsets, requiredRevision, err = enforcedSets.CheckPresenceRequired(naming.Snap(installed.InstanceName))
				// note, this errors out the entire refresh
				if err != nil {
					return err
				}
				// if the snap is already at the required revision then skip it from
				// candidates.
				if !requiredRevision.Unset() && installed.Revision == requiredRevision {
					return nil
				}
			}

			if len(requiredValsets) > 0 {
				setActionValidationSetsAndRequiredRevision(action, requiredValsets, requiredRevision)
			}
		}

		if !action.Revision.Unset() {
			// ignore cohort if revision is specified
			installed.CohortKey = ""
		}

		stateByInstanceName[installed.InstanceName] = snapst

		if len(names) == 0 {
			installed.Block = snapst.Block()
		}

		userID := snapst.UserID
		if userID == 0 {
			userID = fallbackID
		}
		actionsByUserID[userID] = append(actionsByUserID[userID], action)
		if snapst.IgnoreValidation {
			ignoreValidationByInstanceName[installed.InstanceName] = true
		}
		nCands++
		return nil
	}

	holds, err := SnapHolds(st, names)
	if err != nil {
		return nil, nil, nil, err
	}

	// determine current snaps and collect candidates for refresh
	curSnaps, err := collectCurrentSnaps(snapStates, holds, addCand)
	if err != nil {
		return nil, nil, nil, err
	}

	actionsForUser := make(map[*auth.UserState][]*store.SnapAction, len(actionsByUserID))
	noUserActions := actionsByUserID[0]
	for userID, actions := range actionsByUserID {
		if userID == 0 {
			continue
		}
		u, err := userFromUserID(st, userID, 0)
		if err != nil {
			return nil, nil, nil, err
		}
		if u.HasStoreAuth() {
			actionsForUser[u] = actions
		} else {
			noUserActions = append(noUserActions, actions...)
		}
	}
	// coalesce if possible
	if len(noUserActions) != 0 {
		if len(actionsForUser) == 0 {
			actionsForUser[nil] = noUserActions
		} else {
			// coalesce no user actions with one other user's
			for u1, actions := range actionsForUser {
				actionsForUser[u1] = append(actions, noUserActions...)
				break
			}
		}
	}

	// TODO: possibly support a deviceCtx
	theStore := Store(st, nil)

	updates := make([]*snap.Info, 0, nCands)
	for u, actions := range actionsForUser {
		st.Unlock()
		sarsForUser, _, err := theStore.SnapAction(ctx, curSnaps, actions, nil, u, opts)
		st.Lock()
		if err != nil {
			saErr, ok := err.(*store.SnapActionError)
			if !ok {
				return nil, nil, nil, err
			}
			// TODO: use the warning infra here when we have it
			logger.Noticef("%v", saErr)
		}

		for _, sar := range sarsForUser {
			updates = append(updates, sar.Info)
		}
	}

	return updates, stateByInstanceName, ignoreValidationByInstanceName, nil
}

// SnapHolds returns a map of held snaps to lists of holding snaps (including
// "system" for user holds).
func SnapHolds(st *state.State, snaps []string) (map[string][]string, error) {
	allSnapsHoldTime, err := effectiveRefreshHold(st)
	if err != nil {
		return nil, err
	}

	holds, err := HeldSnaps(st, HoldGeneral)
	if err != nil {
		return nil, err
	}

	for _, snap := range snaps {
		if !strutil.ListContains(holds[snap], "system") && allSnapsHoldTime.After(timeNow()) {
			if holds == nil {
				holds = make(map[string][]string)
			}

			holds[snap] = append(holds[snap], "system")
		}
	}

	return holds, nil
}
