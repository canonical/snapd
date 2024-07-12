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

func fallbackUserID(user *auth.UserState) int {
	if !user.HasStoreAuth() {
		return 0
	}
	return user.ID
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

var ErrMissingExpectedResult = fmt.Errorf("unexpectedly empty response from the server (try again later)")

func singleActionResultErr(name, action string, e error) error {
	if e == nil {
		return nil
	}

	if saErr, ok := e.(*store.SnapActionError); ok {
		if len(saErr.Other) != 0 {
			return saErr
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
			return snapErr
		}

		// no result, atypical case
		if saErr.NoResults {
			return ErrMissingExpectedResult
		}
	}

	return e
}

func singleActionResult(name, action string, results []store.SnapActionResult, e error) (store.SnapActionResult, error) {
	if len(results) > 1 {
		return store.SnapActionResult{}, fmt.Errorf("internal error: multiple store results for a single snap op")
	}
	if len(results) > 0 {
		// TODO: if we also have an error log/warn about it
		return results[0], nil
	}

	return store.SnapActionResult{}, singleActionResultErr(name, action, e)
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

		comps, err := snapst.ComponentInfosForRevision(snapInfo.Revision)
		if err != nil {
			return nil, err
		}

		var resources map[string]snap.Revision
		for _, comp := range comps {
			if resources == nil {
				resources = make(map[string]snap.Revision, len(comps))
			}
			resources[comp.Component.ComponentName] = comp.Revision
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
			Resources:        resources,
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

// storeUpdatePlan is a wrapper for storeUpdatePlanCore.
//
// It addresses the case where the store doesn't return refresh candidates for
// snaps with already existing monitored refresh-candidates due to inconsistent
// store return being caused by the throttling.
// A second request is sent for eligible snaps that might have been throttled
// with the RevisionOptions.Scheduled option turned off.
//
// Note: This wrapper is a short term solution and should be removed once a better
// solution is reached.
func storeUpdatePlan(ctx context.Context, st *state.State, allSnaps map[string]*SnapState, requested map[string]StoreUpdate, user *auth.UserState, refreshOpts *store.RefreshOptions, opts Options) (updatePlan, error) {
	// initialize options before using
	refreshOpts, err := refreshOptions(st, refreshOpts)
	if err != nil {
		return updatePlan{}, err
	}

	plan, err := storeUpdatePlanCore(ctx, st, allSnaps, requested, user, refreshOpts, opts)
	if err != nil {
		return updatePlan{}, err
	}

	if !refreshOpts.Scheduled {
		// not an auto-refresh, just return what we got
		return plan, nil
	}

	var oldHints map[string]*refreshCandidate
	if err := st.Get("refresh-candidates", &oldHints); err != nil {
		if errors.Is(err, &state.NoStateError{}) {
			// do nothing
			return plan, nil
		}

		return updatePlan{}, fmt.Errorf("cannot get refresh-candidates: %v", err)
	}

	missingRequests := make(map[string]StoreUpdate)
	for name, hint := range oldHints {
		if !hint.Monitored {
			continue
		}
		hasUpdate := false
		for _, update := range plan.targets {
			if update.info.InstanceName() == name {
				hasUpdate = true
				break
			}
		}
		if hasUpdate {
			continue
		}

		req, ok := requested[name]
		if !ok {
			if !plan.refreshAll() {
				continue
			}
			req = StoreUpdate{InstanceName: name}
		}

		missingRequests[name] = req
	}

	if len(missingRequests) > 0 {
		if err := validateAndInitStoreUpdates(allSnaps, missingRequests, opts); err != nil {
			return updatePlan{}, err
		}

		// mimic manual refresh to avoid throttling.
		// context: snaps may be throttled by the store to balance load
		// and therefore may not always receive an update (even if one was
		// returned before). forcing a manual refresh should be fine since
		// we already started a pre-download for this snap, so no extra
		// load is being exerted on the store.
		refreshOpts.Scheduled = false
		extraPlan, err := storeUpdatePlanCore(ctx, st, allSnaps, missingRequests, user, refreshOpts, opts)
		if err != nil {
			return updatePlan{}, err
		}
		plan.targets = append(plan.targets, extraPlan.targets...)
	}

	return plan, nil
}

func storeUpdatePlanCore(
	ctx context.Context,
	st *state.State,
	allSnaps map[string]*SnapState,
	requested map[string]StoreUpdate,
	user *auth.UserState,
	refreshOpts *store.RefreshOptions,
	opts Options,
) (updatePlan, error) {
	if refreshOpts == nil {
		return updatePlan{}, errors.New("internal error: refresh opts cannot be nil")
	}

	plan := updatePlan{
		requested: make([]string, 0, len(requested)),
	}

	for name := range requested {
		plan.requested = append(plan.requested, name)
	}

	updates := requested
	if plan.refreshAll() {
		updates = make(map[string]StoreUpdate, len(allSnaps))
		for _, snapst := range allSnaps {
			updates[snapst.InstanceName()] = StoreUpdate{
				InstanceName: snapst.InstanceName(),
				// default the channel and cohort key to the existing values,
				RevOpts: RevisionOptions{
					Channel:   snapst.TrackingChannel,
					CohortKey: snapst.CohortKey,
				},
			}
		}
	}

	// if any of the snaps that we are refreshing have components, we need to
	// make sure to explicitly request the components from the store.
	requestComponentsFromStore := false

	// make sure that all requested updates are currently installed
	for _, update := range updates {
		snapst, ok := allSnaps[update.InstanceName]
		if !ok {
			return updatePlan{}, snap.NotInstalledError{Snap: update.InstanceName}
		}

		if snapst.HasActiveComponents() {
			requestComponentsFromStore = true
		}
	}

	enforcedSetsFunc := cachedEnforcedValidationSets(st)

	fallbackID := fallbackUserID(user)

	// hasLocalRevision keeps track of snaps that already have a local revision
	// matching the requested revision. there are two distinct cases here:
	//
	// * the snap might have been requested to be updated but didn't get
	//   updated, either because we detected that the requested/required revision
	//   is already installed, or the store reported that there was no update
	//   available.
	//
	// * we have a local copy of the revision (that was previously installed,
	//   installed, but isn't right now) that is the same as the requested
	//   revision
	//
	// in either case, we need to keep track of these, since we still might need
	// to change the channel, cohort key, or validation set enforcement.
	actionsByUserID, hasLocalRevision, current, err := collectCurrentSnapsAndActions(st, allSnaps, updates, plan.requested, opts, enforcedSetsFunc, fallbackID)
	if err != nil {
		return updatePlan{}, err
	}

	// create actions to refresh (install, from the store's perspective) snaps
	// that were installed locally
	amendActionsByUserID, localAmends, err := installActionsForAmend(st, updates, opts, enforcedSetsFunc, fallbackID)
	if err != nil {
		return updatePlan{}, err
	}

	for _, name := range localAmends {
		hasLocalRevision[allSnaps[name]] = updates[name].RevOpts
	}

	for id, actions := range amendActionsByUserID {
		actionsByUserID[id] = append(actionsByUserID[id], actions...)
	}

	refreshOpts.IncludeResources = requestComponentsFromStore
	sars, noStoreUpdates, err := sendActionsByUserID(ctx, st, actionsByUserID, current, refreshOpts, opts)
	if err != nil {
		return updatePlan{}, err
	}

	for _, name := range noStoreUpdates {
		hasLocalRevision[allSnaps[name]] = updates[name].RevOpts
	}

	for _, sar := range sars {
		up, ok := updates[sar.InstanceName()]
		if !ok {
			return updatePlan{}, fmt.Errorf("unsolicited snap action result: %q", sar.InstanceName())
		}

		snapst, ok := allSnaps[sar.InstanceName()]
		if !ok {
			return updatePlan{}, fmt.Errorf("internal error: snap %q not found", sar.InstanceName())
		}

		currentComps, err := snapst.CurrentComponentInfos()
		if err != nil {
			return updatePlan{}, err
		}

		// build a list of components that are currently installed to then
		// extract from the action results
		compNames := make([]string, 0, len(currentComps))
		for _, comp := range currentComps {
			compNames = append(compNames, comp.Component.ComponentName)
		}

		// compTargets will be filtered down to only the components that appear
		// in the action result, meaning that we might install fewer components
		// than we have installed right now
		compTargets, err := componentTargetsFromActionResult("refresh", sar, compNames)
		if err != nil {
			return updatePlan{}, fmt.Errorf("cannot extract components from snap resources: %w", err)
		}

		// if we still have no channel here, this means that we refreshed
		// by-revision without specifying a channel. make sure we continue to
		// track the channel that the snap is currently on
		up.RevOpts.setChannelIfUnset(snapst.TrackingChannel)

		plan.targets = append(plan.targets, target{
			info:   sar.Info,
			snapst: *snapst,
			setup: SnapSetup{
				DownloadInfo: &sar.DownloadInfo,
				Channel:      up.RevOpts.Channel,
				CohortKey:    up.RevOpts.CohortKey,
			},
			components: compTargets,
		})
	}

	// consider snaps that already have a local copy of the revision that we are
	// trying to install, skipping a trip to the store
	for snapst, revOpts := range hasLocalRevision {
		var si *snap.SideInfo
		if !revOpts.Revision.Unset() {
			si = snapst.Sequence.Revisions[snapst.LastIndex(revOpts.Revision)].Snap
		} else {
			si = snapst.CurrentSideInfo()
		}

		info, err := readInfo(snapst.InstanceName(), si, errorOnBroken)
		if err != nil {
			return updatePlan{}, err
		}

		components, err := componentTargetsFromLocalRevision(snapst, si.Revision)
		if err != nil {
			return updatePlan{}, err
		}

		revOpts.setChannelIfUnset(snapst.TrackingChannel)

		// make sure that we switch the current channel of the snap that we're
		// switching to
		info.Channel = revOpts.Channel

		plan.targets = append(plan.targets, target{
			info:   info,
			snapst: *snapst,
			setup: SnapSetup{
				Channel:   revOpts.Channel,
				CohortKey: revOpts.CohortKey,
				SnapPath:  info.MountFile(),

				// if the caller specified a revision, then we always run
				// through the entire update process. this enables something
				// like "snap refresh --revision=n", where revision n is already
				// installed
				AlwaysUpdate: !revOpts.Revision.Unset(),
			},
			components: components,
		})
	}

	return plan, nil
}

func componentTargetsFromLocalRevision(snapst *SnapState, snapRev snap.Revision) ([]ComponentSetup, error) {
	// TODO:COMPS: for now, go back to the components that were already
	// installed with this revision. to be more robust, we should refresh
	// only the components that are installed with the current revision of
	// the snap. this means we'll need to check with the store for which
	// revisions now available for that snap.
	compInfos, err := snapst.ComponentInfosForRevision(snapRev)
	if err != nil {
		return nil, err
	}

	components := make([]ComponentSetup, 0, len(compInfos))
	for _, compInfo := range compInfos {
		components = append(components, ComponentSetup{
			CompSideInfo: &compInfo.ComponentSideInfo,
			CompType:     compInfo.Type,
		})
	}
	return components, nil
}

func collectCurrentSnapsAndActions(
	st *state.State,
	allSnaps map[string]*SnapState,
	updates map[string]StoreUpdate,
	requested []string,
	opts Options,
	enforcedSets func() (*snapasserts.ValidationSets, error),
	fallbackID int,
) (actionsByUserID map[int][]*store.SnapAction, hasLocalRevision map[*SnapState]RevisionOptions, current []*store.CurrentSnap, err error) {
	hasLocalRevision = make(map[*SnapState]RevisionOptions)
	actionsByUserID = make(map[int][]*store.SnapAction)
	refreshAll := len(requested) == 0

	addCand := func(installed *store.CurrentSnap, snapst *SnapState) error {
		// no auto-refresh for devmode
		if refreshAll && snapst.DevMode {
			return nil
		}

		req, ok := updates[installed.InstanceName]
		if !ok {
			return nil
		}

		// FIXME: snaps that are not active are skipped for now until we know
		// what we want to do
		if !snapst.Active {
			if opts.ExpectOneSnap {
				return fmt.Errorf("refreshing disabled snap %q not supported", snapst.InstanceName())
			}
			return nil
		}

		if !req.RevOpts.Revision.Unset() && snapst.LastIndex(req.RevOpts.Revision) != -1 {
			hasLocalRevision[snapst] = req.RevOpts
			return nil
		}

		action := &store.SnapAction{
			Action:       "refresh",
			SnapID:       installed.SnapID,
			InstanceName: installed.InstanceName,
		}

		// TODO: this is silly, but it matches how we currently send these flags
		// now. we should probably just default to sending enforce, but that
		// would require updating a good number of tests. good candidate for a
		// follow-up PR.
		//
		// if we are expecting only one snap to be updated, we respect the flag
		// that was passed in. this maintains the existing behavior of Update vs
		// UpdateMany.
		ignoreValidation := snapst.IgnoreValidation
		if opts.ExpectOneSnap {
			ignoreValidation = opts.Flags.IgnoreValidation
			if !opts.Flags.IgnoreValidation && req.RevOpts.Revision.Unset() {
				action.Flags = store.SnapActionEnforceValidation
			}
		}

		if err := completeStoreAction(action, req.RevOpts, ignoreValidation, enforcedSets); err != nil {
			return err
		}

		// if we already have the requested revision installed, we don't need to
		// consider this snap for a store update, but we still should return it
		// as a target for potentially switching channels or cohort keys
		if !action.Revision.Unset() && action.Revision == installed.Revision {
			hasLocalRevision[snapst] = req.RevOpts
			return nil
		}

		if !action.Revision.Unset() {
			// ignore cohort if revision is specified
			installed.CohortKey = ""
		}

		// only enforce refresh block if we are refreshing everything
		if refreshAll {
			installed.Block = snapst.Block()
		}

		userID := snapst.UserID
		if userID == 0 {
			userID = fallbackID
		}
		actionsByUserID[userID] = append(actionsByUserID[userID], action)

		return nil
	}

	// TODO: is this right? why do we only pass in the requested names here?
	// what about when we are refreshing all snaps?
	holds, err := SnapHolds(st, requested)
	if err != nil {
		return nil, nil, nil, err
	}

	// determine current snaps and create actions for each snap that needs to
	// be refreshed
	current, err = collectCurrentSnaps(allSnaps, holds, addCand)
	if err != nil {
		return nil, nil, nil, err
	}

	return actionsByUserID, hasLocalRevision, current, nil
}

func installActionsForAmend(st *state.State, updates map[string]StoreUpdate, opts Options, enforcedSets func() (*snapasserts.ValidationSets, error), fallbackID int) (map[int][]*store.SnapAction, []string, error) {
	actionsByUserID := make(map[int][]*store.SnapAction)
	var localAmends []string
	for _, up := range updates {
		var snapst SnapState
		if err := Get(st, up.InstanceName, &snapst); err != nil {
			return nil, nil, err
		}

		si := snapst.CurrentSideInfo()

		if si == nil || si.SnapID != "" || snapst.TryMode {
			continue
		}

		// we allow changing snap revisions of a local-only snap without the
		// --amend flag as long as we already have had the revision installed
		if !up.RevOpts.Revision.Unset() && snapst.LastIndex(up.RevOpts.Revision) != -1 {
			localAmends = append(localAmends, snapst.InstanceName())
			continue
		}

		if !opts.Flags.Amend {
			if opts.ExpectOneSnap {
				return nil, nil, store.ErrLocalSnap
			}
			continue
		}

		info, err := snapst.CurrentInfo()
		if err != nil {
			return nil, nil, err
		}

		comps, err := snapst.CurrentComponentInfos()
		if err != nil {
			return nil, nil, err
		}

		// TODO: lift this restriction, just don't want to think about it right
		// now
		if len(comps) > 0 {
			return nil, nil, fmt.Errorf("cannot amend snap %q with components", up.InstanceName)
		}

		action := &store.SnapAction{
			Action:       "install",
			InstanceName: info.InstanceName(),
			Epoch:        info.Epoch,
		}

		ignoreValidation := snapst.IgnoreValidation
		if opts.ExpectOneSnap {
			ignoreValidation = opts.Flags.IgnoreValidation
		}

		if err := completeStoreAction(action, up.RevOpts, ignoreValidation, enforcedSets); err != nil {
			return nil, nil, err
		}

		userID := snapst.UserID
		if userID == 0 {
			userID = fallbackID
		}
		actionsByUserID[userID] = append(actionsByUserID[userID], action)
	}

	return actionsByUserID, localAmends, nil
}

func sendActionsByUserID(ctx context.Context, st *state.State, actionsByUserID map[int][]*store.SnapAction, current []*store.CurrentSnap, refreshOpts *store.RefreshOptions, opts Options) (sars []store.SnapActionResult, noUpdatesAvailable []string, err error) {
	actionsForUser := make(map[*auth.UserState][]*store.SnapAction, len(actionsByUserID))
	noUserActions := actionsByUserID[0]
	for userID, actions := range actionsByUserID {
		if userID == 0 {
			continue
		}

		u, err := userFromUserID(st, userID, 0)
		if err != nil {
			return nil, nil, err
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

	sto := Store(st, opts.DeviceCtx)

	for u, actions := range actionsForUser {
		st.Unlock()
		perUserSars, _, err := sto.SnapAction(ctx, current, actions, nil, u, refreshOpts)
		st.Lock()

		if err != nil {
			saErr, ok := err.(*store.SnapActionError)
			if !ok {
				return nil, nil, err
			}

			if opts.ExpectOneSnap && saErr.NoResults {
				return nil, nil, ErrMissingExpectedResult
			}

			// save these, since we still have things to do with snaps that
			// might not have a new revision available
			for name, e := range combineErrs(saErr) {
				if !errors.Is(e, store.ErrNoUpdateAvailable) && opts.ExpectOneSnap {
					_, _, err := saErr.SingleOpError()
					return nil, nil, err
				}

				noUpdatesAvailable = append(noUpdatesAvailable, name)
			}

			logger.Noticef("%v", saErr)
		}

		sars = append(sars, perUserSars...)
	}

	return sars, noUpdatesAvailable, nil
}

func combineErrs(saErr *store.SnapActionError) map[string]error {
	errs := make(map[string]error, len(saErr.Refresh)+len(saErr.Install)+len(saErr.Download))
	for name, e := range saErr.Refresh {
		errs[name] = e
	}
	for name, e := range saErr.Install {
		errs[name] = e
	}
	for name, e := range saErr.Download {
		errs[name] = e
	}
	return errs
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
