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
	"sort"

	"golang.org/x/net/context"

	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/overlord/auth"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/store"
	"github.com/snapcore/snapd/strutil"
)

type updateInfoOpts struct {
	channel          string
	ignoreValidation bool
	amend            bool
}

func idForUser(user *auth.UserState) int {
	if user == nil {
		return 0
	}
	return user.ID
}

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

// userFromUserIDOrFallback returns the user corresponding to userID
// if valid or otherwise the fallbackUser.
func userFromUserIDOrFallback(st *state.State, userID int, fallbackUser *auth.UserState) (*auth.UserState, error) {
	if userID != 0 {
		u, err := auth.User(st, userID)
		if err != nil && err != auth.ErrInvalidUser {
			return nil, err
		}
		if err == nil {
			return u, nil
		}
	}
	return fallbackUser, nil
}

func snapNameToID(st *state.State, name string, user *auth.UserState) (string, error) {
	theStore := Store(st)
	st.Unlock()
	info, err := theStore.SnapInfo(store.SnapSpec{Name: name}, user)
	st.Lock()
	return info.SnapID, err
}

func snapInfo(st *state.State, name, channel string, revision snap.Revision, userID int) (*snap.Info, error) {
	user, err := userFromUserID(st, userID)
	if err != nil {
		return nil, err
	}
	theStore := Store(st)
	st.Unlock() // calls to the store should be done without holding the state lock
	spec := store.SnapSpec{
		Name:     name,
		Channel:  channel,
		Revision: revision,
	}
	snap, err := theStore.SnapInfo(spec, user)
	st.Lock()
	return snap, err
}

func updateInfo(st *state.State, snapst *SnapState, opts *updateInfoOpts, userID int) (*snap.Info, error) {
	if opts == nil {
		opts = &updateInfoOpts{}
	}

	installedCtxt, err := installedContext(st)
	if err != nil {
		return nil, err
	}

	curInfo, user, err := preUpdateInfo(st, snapst, opts.amend, userID)
	if err != nil {
		return nil, err
	}

	var flags store.InstallRefreshActionFlags
	if opts.ignoreValidation {
		flags = store.InstallRefreshIgnoreValidation
	} else {
		flags = store.InstallRefreshEnforceValidation
	}

	action := &store.InstallRefreshAction{
		Action: "refresh",
		SnapID: curInfo.SnapID,
		// the desired channel
		Channel: opts.channel,
		Flags:   flags,
	}

	if curInfo.SnapID == "" { // amend
		action.Action = "install"
		action.Name = curInfo.Name()
	}

	theStore := Store(st)
	st.Unlock() // calls to the store should be done without holding the state lock
	res, err := theStore.InstallRefresh(context.TODO(), installedCtxt, []*store.InstallRefreshAction{action}, user, nil)
	st.Lock()

	return singleActionResult(curInfo.Name(), action.Action, res, err)
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

func singleActionResult(name, action string, results []*snap.Info, e error) (info *snap.Info, err error) {
	if len(results) > 0 {
		// TODO: if we also have an error log/warn about it
		return results[0], nil
	}

	if irErr, ok := e.(*store.InstallRefreshError); ok {
		if len(irErr.Other) != 0 {
			return nil, irErr
		}

		var snapErr error
		switch action {
		case "refresh":
			snapErr = irErr.Refresh[name]
		case "install":
			snapErr = irErr.Install[name]
		}
		if snapErr != nil {
			return nil, snapErr
		}

		// no result, atypical case
		if irErr.NoResults {
			switch action {
			case "refresh":
				return nil, store.ErrNoUpdateAvailable
			case "install":
				return nil, store.ErrSnapNotFound
			}
		}
	}

	return nil, e
}

func updateToRevisionInfo(st *state.State, snapst *SnapState, channel string, revision snap.Revision, userID int) (*snap.Info, error) {
	curInfo, user, err := preUpdateInfo(st, snapst, false, userID)
	if err != nil {
		return nil, err
	}

	theStore := Store(st)
	st.Unlock() // calls to the store should be done without holding the state lock
	spec := store.SnapSpec{
		Name:     curInfo.Name(),
		Channel:  channel,
		Revision: revision,
	}
	snap, err := theStore.SnapInfo(spec, user)
	st.Lock()
	return snap, err
}

func installedContext(st *state.State) ([]*store.CurrentSnap, error) {
	snapStates, err := All(st)
	if err != nil {
		return nil, err
	}

	installedCtxt := make([]*store.CurrentSnap, 0, len(snapStates))

	for snapName, snapst := range snapStates {
		if snapst.TryMode {
			// do not report try-mode snaps
			continue
		}

		snapInfo, err := snapst.CurrentInfo()
		if err != nil {
			// log something maybe?
			continue
		}

		if snapInfo.SnapID == "" {
			// not a store snap
			continue
		}

		installed := &store.CurrentSnap{
			Name:             snapName,
			SnapID:           snapInfo.SnapID,
			TrackingChannel:  snapst.Channel,
			Revision:         snapInfo.Revision,
			RefreshedDate:    revisionDate(snapInfo),
			IgnoreValidation: snapst.IgnoreValidation,
		}
		installedCtxt = append(installedCtxt, installed)
	}

	return installedCtxt, nil
}

func refreshCandidates(ctx context.Context, st *state.State, names []string, user *auth.UserState, opts *store.RefreshOptions) ([]*snap.Info, map[string]*SnapState, map[string]bool, error) {
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

	sort.Strings(names)

	installedCtxt := make([]*store.CurrentSnap, 0, len(snapStates))
	actions := make([]*store.InstallRefreshAction, 0, len(names))
	stateByID := make(map[string]*SnapState, len(snapStates))
	ignoreValidation := make(map[string]bool)
	userIDs := make(map[int]bool)
	for snapName, snapst := range snapStates {
		// XXX: fix/move this
		if len(names) == 0 && (snapst.TryMode || snapst.DevMode) {
			// no auto-refresh for trymode nor devmode
			continue
		}

		// XXX: fix/move this
		// FIXME: snaps that are not active are skipped for now
		//        until we know what we want to do
		if !snapst.Active {
			continue
		}

		snapInfo, err := snapst.CurrentInfo()
		if err != nil {
			// log something maybe?
			continue
		}

		if snapInfo.SnapID == "" {
			// no refresh for sideloaded
			continue
		}

		installed := &store.CurrentSnap{
			Name: snapName,
			// the desired channel (not info.Channel!)
			TrackingChannel:  snapst.Channel,
			SnapID:           snapInfo.SnapID,
			Revision:         snapInfo.Revision,
			RefreshedDate:    revisionDate(snapInfo),
			IgnoreValidation: snapst.IgnoreValidation,
		}
		installedCtxt = append(installedCtxt, installed)

		if len(names) > 0 && !strutil.SortedListContains(names, snapInfo.Name()) {
			continue
		}

		stateByID[snapInfo.SnapID] = snapst

		if len(names) == 0 {
			installed.Block = snapst.Block()
		}

		actions = append(actions, &store.InstallRefreshAction{
			Action: "refresh",
			SnapID: snapInfo.SnapID,
		})

		if snapst.UserID != 0 {
			userIDs[snapst.UserID] = true
		}
		if snapst.IgnoreValidation {
			ignoreValidation[snapInfo.SnapID] = true
		}
	}

	theStore := Store(st)

	// XXX: address TODO
	// TODO: we query for all snaps for each user so that the
	// store can take into account validation constraints, we can
	// do better with coming APIs
	updatesInfo := make(map[string]*snap.Info, len(actions))
	fallbackUsed := false
	fallbackID := idForUser(user)
	if len(userIDs) == 0 {
		// none of the snaps had an installed user set, just
		// use the fallbackID
		userIDs[fallbackID] = true
	}
	for userID := range userIDs {
		u, err := userFromUserIDOrFallback(st, userID, user)
		if err != nil {
			return nil, nil, nil, err
		}
		// consider the fallback user at most once
		if idForUser(u) == fallbackID {
			if fallbackUsed {
				continue
			}
			fallbackUsed = true
		}

		st.Unlock()
		updatesForUser, err := theStore.InstallRefresh(ctx, installedCtxt, actions, u, opts)
		st.Lock()
		if err != nil {
			irErr, ok := err.(*store.InstallRefreshError)
			if !ok {
				return nil, nil, nil, err
			}
			// TODO: use the warning infra here when we have it
			logger.Noticef("%v", irErr)
		}

		for _, snapInfo := range updatesForUser {
			updatesInfo[snapInfo.SnapID] = snapInfo
		}
	}

	updates := make([]*snap.Info, 0, len(updatesInfo))
	for _, snapInfo := range updatesInfo {
		updates = append(updates, snapInfo)
	}

	return updates, stateByID, ignoreValidation, nil
}
