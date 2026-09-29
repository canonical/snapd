// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2026 Canonical Ltd
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
	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/snap/channel"
	"github.com/snapcore/snapd/snap/uctrack"
	"github.com/snapcore/snapd/store"
)

// ubuntuCoreTracksChannel is the agreed source of truth for
// [snap.Info.UbuntuCoreTracks]. Master publishes latest/stable, so that
// snap.yaml is the most up-to-date map. A fips track or a pinned revision
// is not consulted.
const ubuntuCoreTracksChannel = "latest/stable"

// installedSnapdTrackingChannel is the channel the installed snapd snap is
// tracking. It is empty when snapd is not installed.
func installedSnapdTrackingChannel(allSnaps map[string]*SnapState) string {
	snapst := allSnaps["snapd"]
	if snapst == nil || !snapst.IsInstalled() {
		return ""
	}
	return snapst.TrackingChannel
}

// resolveSnapdUCTrackChannel resolves the snapd channel for model using the
// latest/stable ubuntu-core-tracks from [latestStableSnapdTracks] and
// [uctrack.Resolve]. trackingChannel is [installedSnapdTrackingChannel] and is
// required: an install has none, so it is not resolved. requestedChannel is
// the channel for this operation. It may be empty, which resolves the tracking
// channel, including a revision refresh. The state lock must be held; it is
// released for the store round-trip. Callers keep their channel on
// [uctrack.ErrNotApplicable]. Any other error fails the operation.
func resolveSnapdUCTrackChannel(ctx context.Context, st *state.State, trackingChannel, requestedChannel string, model *asserts.Model, sto StoreService, userID int) (snapdUCTrackChannel string, err error) {
	// [uctrack.SystemBootBaseApplicable] rejects classic, hybrid, a non-core
	// base, and Ubuntu Core 16 from the model alone. The track map cannot
	// change that.
	if _, err := uctrack.SystemBootBaseApplicable(model); err != nil {
		return "", err
	}

	// An install has nothing tracked, so there is no track to resolve.
	if trackingChannel == "" {
		return "", fmt.Errorf("%w: empty tracking channel", uctrack.ErrNotApplicable)
	}

	tracks, err := latestStableSnapdTracks(ctx, st, sto, userID)
	if err != nil {
		return "", err
	}

	snapdUCTrackChannel, err = uctrack.Resolve(model, trackingChannel, requestedChannel, tracks)
	if err != nil {
		return "", err
	}

	// Name the channel that was resolved. A revision refresh has no requested
	// channel, so the notice would otherwise say it resolved from "".
	fromChannel := requestedChannel
	if fromChannel == "" {
		fromChannel = trackingChannel
	}
	if !storeChannelsEqual(fromChannel, snapdUCTrackChannel) {
		logger.Noticef("resolved snapd channel from %q to %q to follow Ubuntu Core tracks", fromChannel, snapdUCTrackChannel)
	}
	return snapdUCTrackChannel, nil
}

// latestStableSnapdTracks returns ubuntu-core-tracks from the latest/stable
// snapd snap. The action is an install with no revision, so a refresh of an
// already-current latest/stable cannot hide the snap. It sends no installed
// snaps and no refresh options: the action names snapd and latest/stable.
// The userID selects the store session.
func latestStableSnapdTracks(ctx context.Context, st *state.State, sto StoreService, userID int) (snap.UbuntuCoreTracks, error) {
	action := &store.SnapAction{
		Action:       "install",
		InstanceName: "snapd",
		Channel:      ubuntuCoreTracksChannel,
	}

	user, err := userFromUserID(st, userID)
	if err != nil {
		return nil, err
	}

	st.Unlock()
	// The installed-snap list is nil because this install names snapd and
	// latest/stable itself. That list is how a refresh matches an installed
	// snap, and that match can hide the revision whose snap.yaml is the map.
	// opts is nil because [store.SnapAction] turns that into an empty
	// [store.RefreshOptions]: no schedule, no refresh-control, and no
	// component request. The caller's user is unchanged.
	results, _, err := sto.SnapAction(ctx, nil, []*store.SnapAction{action}, nil, user, nil)
	st.Lock()

	if err != nil {
		return nil, singleActionResultErr("snapd", "install", err)
	}
	if len(results) != 1 || results[0].Info == nil {
		return nil, errors.New("cannot read ubuntu-core-tracks: no snapd result from latest/stable")
	}
	if results[0].Info.Type() != snap.TypeSnapd {
		return nil, fmt.Errorf("cannot read ubuntu-core-tracks: latest/stable snapd snap has type %q", results[0].Info.Type())
	}
	// A non-empty map was already validated while parsing snap.yaml. An empty
	// or nil map stays nil; [uctrack.Resolve] treats that as no map.
	return results[0].Info.UbuntuCoreTracks, nil
}

// restoreSnapdUCTrackChannel puts snapdUCTrackChannel back because a
// validation set pin clears the action channel in [completeStoreAction]. The
// pinned revision is still requested on that track.
func restoreSnapdUCTrackChannel(snapAction *store.SnapAction, snapdUCTrackChannel string) {
	if snapAction.InstanceName == "snapd" && snapdUCTrackChannel != "" {
		snapAction.Channel = snapdUCTrackChannel
	}
}

// requireSnapdEffectiveChannel keeps tracking on snapdUCTrackChannel, the
// channel the store was asked for. A refresh already ignores
// [store.SnapActionResult.RedirectChannel]. An install adopts it, so the
// redirect is cleared. The revision's effective channel is [snap.Info.Channel].
// When the store reports one, it must name that requested channel. An empty
// effective channel is left alone: a revision request does not carry one.
func requireSnapdEffectiveChannel(sar *store.SnapActionResult, snapdUCTrackChannel string) error {
	sar.RedirectChannel = ""
	if sar.Info == nil || sar.Channel == "" {
		return nil
	}
	if storeChannelsEqual(sar.Channel, snapdUCTrackChannel) {
		return nil
	}
	return fmt.Errorf("cannot follow Ubuntu Core track %q: store reports effective channel %q", snapdUCTrackChannel, sar.Channel)
}

// storeChannelsEqual reports whether a and b name the same store channel.
// [channel.Parse] drops an implied "latest/" track, so "stable" and
// "latest/stable" are equal. A branch or a different risk is not.
func storeChannelsEqual(a, b string) bool {
	ca, err1 := channel.Parse(a, "-")
	cb, err2 := channel.Parse(b, "-")
	if err1 != nil || err2 != nil {
		return a == b
	}
	return ca.String() == cb.String()
}
