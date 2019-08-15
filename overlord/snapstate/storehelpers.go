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
	"fmt"
	"sort"

	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/overlord/auth"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/store"
	"github.com/snapcore/snapd/strutil"
)

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

	if err := st.Get("refresh-privacy-key", &opts.PrivacyKey); err != nil && err != state.ErrNoState {
		return nil, fmt.Errorf("cannot obtain store request salt: %v", err)
	}
	if opts.PrivacyKey == "" {
		return nil, fmt.Errorf("internal error: request salt is unset")
	}
	return &opts, nil
}

func installInfo(ctx context.Context, st *state.State, name string, revOpts *RevisionOptions, userID int, deviceCtx DeviceContext) (*snap.Info, error) {
	// TODO: support ignore-validation?

	curSnaps, err := currentSnaps(st)
	if err != nil {
		return nil, err
	}

	user, err := userFromUserID(st, userID)
	if err != nil {
		return nil, err
	}

	opts, err := refreshOptions(st, nil)
	if err != nil {
		return nil, err
	}

	action := &store.SnapAction{
		Action:       "install",
		InstanceName: name,
	}

	// cannot specify both with the API
	if revOpts.Revision.Unset() {
		// the desired channel
		action.Channel = revOpts.Channel
		// the desired cohort key
		action.CohortKey = revOpts.CohortKey
	} else {
		// the desired revision
		action.Revision = revOpts.Revision
	}

	theStore := Store(st, deviceCtx)
	st.Unlock() // calls to the store should be done without holding the state lock
	res, err := theStore.SnapAction(ctx, curSnaps, []*store.SnapAction{action}, user, opts)
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
		Channel:   opts.Channel,
		CohortKey: opts.CohortKey,
		Flags:     storeFlags,
	}

	if curInfo.SnapID == "" { // amend
		action.Action = "install"
		action.Epoch = curInfo.Epoch
	}

	theStore := Store(st, deviceCtx)
	st.Unlock() // calls to the store should be done without holding the state lock
	res, err := theStore.SnapAction(context.TODO(), curSnaps, []*store.SnapAction{action}, user, refreshOpts)
	st.Lock()

	return singleActionResult(curInfo.InstanceName(), action.Action, res, err)
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

func singleActionResult(name, action string, results []*snap.Info, e error) (info *snap.Info, err error) {
	if len(results) > 1 {
		return nil, fmt.Errorf("internal error: multiple store results for a single snap op")
	}
	if len(results) > 0 {
		// TODO: if we also have an error log/warn about it
		return results[0], nil
	}

	if saErr, ok := e.(*store.SnapActionError); ok {
		if len(saErr.Other) != 0 {
			return nil, saErr
		}

		var snapErr error
		switch action {
		case "refresh":
			snapErr = saErr.Refresh[name]
		case "install":
			snapErr = saErr.Install[name]
		}
		if snapErr != nil {
			return nil, snapErr
		}

		// no result, atypical case
		if saErr.NoResults {
			return nil, ErrMissingExpectedResult
		}
	}

	return nil, e
}

func updateToRevisionInfo(st *state.State, snapst *SnapState, revision snap.Revision, userID int, deviceCtx DeviceContext) (*snap.Info, error) {
	// TODO: support ignore-validation?

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
		Revision: revision,
	}

	theStore := Store(st, deviceCtx)
	st.Unlock() // calls to the store should be done without holding the state lock
	res, err := theStore.SnapAction(context.TODO(), curSnaps, []*store.SnapAction{action}, user, opts)
	st.Lock()

	return singleActionResult(curInfo.InstanceName(), action.Action, res, err)
}

func currentSnaps(st *state.State) ([]*store.CurrentSnap, error) {
	snapStates, err := All(st)
	if err != nil {
		return nil, err
	}

	if len(snapStates) == 0 {
		// no snaps installed, do not bother any further
		return nil, nil
	}

	curSnaps := collectCurrentSnaps(snapStates, nil)
	return curSnaps, nil
}

func collectCurrentSnaps(snapStates map[string]*SnapState, consider func(*store.CurrentSnap, *SnapState)) (curSnaps []*store.CurrentSnap) {
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
			TrackingChannel:  snapst.Channel,
			Revision:         snapInfo.Revision,
			RefreshedDate:    revisionDate(snapInfo),
			IgnoreValidation: snapst.IgnoreValidation,
			Epoch:            snapInfo.Epoch,
			CohortKey:        snapst.CohortKey,
		}
		curSnaps = append(curSnaps, installed)

		if consider != nil {
			consider(installed, snapst)
		}
	}

	return curSnaps
}

func refreshCandidates(ctx context.Context, st *state.State, names []string, user *auth.UserState, opts *store.RefreshOptions) ([]*snap.Info, map[string]*SnapState, map[string]bool, error) {
	snapStates, err := All(st)
	if err != nil {
		return nil, nil, nil, err
	}

	opts, err = refreshOptions(st, opts)
	if err != nil {
		return nil, nil, nil, err
	}

	// check if we have this name at all
	for _, name := range names {
		if _, ok := snapStates[name]; !ok {
			return nil, nil, nil, snap.NotInstalledError{Snap: name}
		}
	}

	sort.Strings(names)

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

	addCand := func(installed *store.CurrentSnap, snapst *SnapState) {
		// FIXME: snaps that are not active are skipped for now
		//        until we know what we want to do
		if !snapst.Active {
			return
		}

		if len(names) == 0 && snapst.DevMode {
			// no auto-refresh for devmode
			return
		}

		if len(names) > 0 && !strutil.SortedListContains(names, installed.InstanceName) {
			return
		}

		stateByInstanceName[installed.InstanceName] = snapst

		if len(names) == 0 {
			installed.Block = snapst.Block()
		}

		userID := snapst.UserID
		if userID == 0 {
			userID = fallbackID
		}
		actionsByUserID[userID] = append(actionsByUserID[userID], &store.SnapAction{
			Action:       "refresh",
			SnapID:       installed.SnapID,
			InstanceName: installed.InstanceName,
		})
		if snapst.IgnoreValidation {
			ignoreValidationByInstanceName[installed.InstanceName] = true
		}
		nCands++
	}
	// determine current snaps and collect candidates for refresh
	curSnaps := collectCurrentSnaps(snapStates, addCand)

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
		updatesForUser, err := theStore.SnapAction(ctx, curSnaps, actions, u, opts)
		st.Lock()
		if err != nil {
			saErr, ok := err.(*store.SnapActionError)
			if !ok {
				return nil, nil, nil, err
			}
			// TODO: use the warning infra here when we have it
			logger.Noticef("%v", saErr)
		}

		updates = append(updates, updatesForUser...)
	}

	return updates, stateByInstanceName, ignoreValidationByInstanceName, nil
}

func installCandidates(st *state.State, names []string, channel string, user *auth.UserState) ([]*snap.Info, error) {
	curSnaps, err := currentSnaps(st)
	if err != nil {
		return nil, err
	}

	opts, err := refreshOptions(st, nil)
	if err != nil {
		return nil, err
	}

	actions := make([]*store.SnapAction, len(names))
	for i, name := range names {
		actions[i] = &store.SnapAction{
			Action:       "install",
			InstanceName: name,
			// the desired channel
			Channel: channel,
		}
	}

	// TODO: possibly support a deviceCtx
	theStore := Store(st, nil)
	st.Unlock() // calls to the store should be done without holding the state lock
	defer st.Lock()
	return theStore.SnapAction(context.TODO(), curSnaps, actions, user, opts)
}
