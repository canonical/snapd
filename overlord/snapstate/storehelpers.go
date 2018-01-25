// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2016-2017 Canonical Ltd
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
	"fmt"
	"sort"

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

	curInfo, user, err := preUpdateInfo(st, snapst, opts.amend, userID)
	if err != nil {
		return nil, err
	}

	refreshCand := &store.RefreshCandidate{
		// the desired channel
		Channel:          opts.channel,
		SnapID:           curInfo.SnapID,
		Revision:         curInfo.Revision,
		Epoch:            curInfo.Epoch,
		IgnoreValidation: opts.ignoreValidation,
		Amend:            opts.amend,
	}

	theStore := Store(st)
	st.Unlock() // calls to the store should be done without holding the state lock
	res, err := theStore.LookupRefresh(refreshCand, user)
	st.Lock()
	return res, err
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

		// in amend mode we need to move to the store rev
		id, err := snapNameToID(st, curInfo.Name(), user)
		if err != nil {
			return nil, nil, fmt.Errorf("cannot get snap ID for %q: %v", curInfo.Name(), err)
		}
		curInfo.SnapID = id
		// set revision to "unknown"
		curInfo.Revision = snap.R(0)
	}

	return curInfo, user, nil
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

func refreshCandidates(st *state.State, names []string, user *auth.UserState, flags *store.RefreshOptions) ([]*snap.Info, map[string]*SnapState, map[string]bool, error) {
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

	stateByID := make(map[string]*SnapState, len(snapStates))
	candidatesInfo := make([]*store.RefreshCandidate, 0, len(snapStates))
	ignoreValidation := make(map[string]bool)
	userIDs := make(map[int]bool)
	for _, snapst := range snapStates {
		if len(names) == 0 && (snapst.TryMode || snapst.DevMode) {
			// no auto-refresh for trymode nor devmode
			continue
		}

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

		if len(names) > 0 && !strutil.SortedListContains(names, snapInfo.Name()) {
			continue
		}

		stateByID[snapInfo.SnapID] = snapst

		// get confinement preference from the snapstate
		candidateInfo := &store.RefreshCandidate{
			// the desired channel (not info.Channel!)
			Channel:          snapst.Channel,
			SnapID:           snapInfo.SnapID,
			Revision:         snapInfo.Revision,
			Epoch:            snapInfo.Epoch,
			IgnoreValidation: snapst.IgnoreValidation,
		}

		if len(names) == 0 {
			candidateInfo.Block = snapst.Block()
		}

		candidatesInfo = append(candidatesInfo, candidateInfo)
		if snapst.UserID != 0 {
			userIDs[snapst.UserID] = true
		}
		if snapst.IgnoreValidation {
			ignoreValidation[snapInfo.SnapID] = true
		}
	}

	theStore := Store(st)

	// TODO: we query for all snaps for each user so that the
	// store can take into account validation constraints, we can
	// do better with coming APIs
	updatesInfo := make(map[string]*snap.Info, len(candidatesInfo))
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
		updatesForUser, err := theStore.ListRefresh(candidatesInfo, u, flags)
		st.Lock()
		if err != nil {
			return nil, nil, nil, err
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
