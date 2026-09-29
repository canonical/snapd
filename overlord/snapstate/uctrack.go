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
	"github.com/snapcore/snapd/snap/naming"
	"github.com/snapcore/snapd/snap/uctrack"
	"github.com/snapcore/snapd/store"
)

// maybeRedirectSnapdTrack applies Ubuntu Core track policy after the first
// store round-trip, which supplies snap.yaml's ubuntu-core-tracks map. When
// the channel remaps, a second SnapAction fetches the revision to install,
// refresh, or download.
//
// Only the snapd snap is considered. Classic, hybrid, UC16, a boot base that
// is not in the map, and an unknown track keep the first result.
// [uctrack.Resolve] keeps the risk and drops the branch.
//
// A pinned or requested revision is queried on the mapped track. localOnly
// means a refresh found no update there: keep the installed revision and
// switch tracking to the mapped channel. includeResources requests component
// resources on the second action.
//
// The state lock must be held by the caller. It is released for the second
// store round-trip.
func maybeRedirectSnapdTrack(ctx context.Context, st *state.State, sar store.SnapActionResult, revOpts *RevisionOptions, snapst *SnapState, opts Options, action string, includeResources bool) (store.SnapActionResult, bool, error) {
	if revOpts == nil || snapst == nil {
		return store.SnapActionResult{}, false, errors.New("internal error: snapd track redirect is missing revision options or snap state")
	}
	if sar.Info == nil || sar.Info.Type() != snap.TypeSnapd || sar.InstanceName().String() != "snapd" {
		return sar, false, nil
	}

	model, err := deviceModelForSnapdTrack(st, opts)
	if err != nil {
		return store.SnapActionResult{}, false, err
	}
	if model == nil {
		return sar, false, nil
	}

	inputChannel := revOpts.Channel
	if inputChannel == "" {
		if snapst.TrackingChannel != "" {
			inputChannel = snapst.TrackingChannel
		} else {
			inputChannel = "stable"
		}
	}

	mapped, err := uctrack.Resolve(model, inputChannel, sar.Info.UbuntuCoreTracks)
	if err != nil {
		if errors.Is(err, uctrack.ErrNotApplicable) ||
			errors.Is(err, uctrack.ErrBootBaseNotCovered) ||
			errors.Is(err, uctrack.ErrNoTrack) {
			return sar, false, nil
		}
		return store.SnapActionResult{}, false, err
	}

	if revOpts.ValidationSets != nil {
		pres, perr := revOpts.ValidationSets.Presence(naming.Snap("snapd"))
		if perr != nil {
			return store.SnapActionResult{}, false, perr
		}
		if !pres.Revision.Unset() && revOpts.Revision.Unset() {
			revOpts.Revision = pres.Revision
		}
	}

	alreadyMapped := storeChannelsEqual(inputChannel, mapped)
	if alreadyMapped && revOpts.Revision.Unset() {
		return sar, false, nil
	}

	switch action {
	case "install", "refresh", "download":
	default:
		return store.SnapActionResult{}, false, fmt.Errorf("internal error: unexpected store action %q for snapd track redirect", action)
	}

	if !alreadyMapped {
		logger.Noticef("remapping snapd from %q to %q to follow Ubuntu Core tracks", inputChannel, mapped)
	}
	revOpts.Channel = mapped

	sa := &store.SnapAction{
		Action:       action,
		InstanceName: sar.InstanceName().String(),
	}
	if action == "refresh" {
		sa.SnapID = sar.SnapID
		if sa.SnapID == "" {
			if si := snapst.CurrentSideInfo(); si != nil {
				sa.SnapID = si.SnapID
			}
		}
		if sa.SnapID == "" {
			return store.SnapActionResult{}, false, errors.New("internal error: cannot refresh snapd onto an Ubuntu Core track without a snap id")
		}
	}
	if action == "install" && snapst.IsInstalled() {
		// An amend install carries the installed epoch, matching the first action.
		info, err := snapst.CurrentInfo()
		if err != nil {
			return store.SnapActionResult{}, false, err
		}
		sa.Epoch = info.Epoch
	}

	ignoreValidation := opts.Flags.IgnoreValidation
	if action == "refresh" {
		ignoreValidation = ignoreValidationSetsForRefresh(snapst, opts)
	}

	// The second lookup is not a scheduled refresh. A throttled first
	// response must not hide the revision on the mapped track.
	newSAR, err := sendOneStoreAction(ctx, st, sa, *revOpts, snapst, opts, includeResources, ignoreValidation, mapped)
	if err != nil {
		if action == "refresh" && errors.Is(err, store.ErrNoUpdateAvailable) {
			return store.SnapActionResult{}, true, nil
		}
		return store.SnapActionResult{}, false, err
	}

	if err := rejectSnapdTrackRedirect(&newSAR, mapped, action); err != nil {
		return store.SnapActionResult{}, false, err
	}
	return newSAR, false, nil
}

// deviceModelForSnapdTrack returns the model to consult for track policy.
// A remodel context on opts wins. No model yet is not an error: policy does
// not apply and the caller keeps the first store result.
func deviceModelForSnapdTrack(st *state.State, opts Options) (*asserts.Model, error) {
	deviceCtx := opts.DeviceCtx
	if deviceCtx == nil {
		if DeviceCtx == nil {
			return nil, nil
		}
		var err error
		deviceCtx, err = DeviceCtx(st, nil, nil)
		if errors.Is(err, state.ErrNoState) {
			return nil, nil
		}
		if err != nil {
			return nil, err
		}
	}
	if deviceCtx == nil {
		return nil, nil
	}
	return deviceCtx.Model(), nil
}

// sendOneStoreAction sends a single already-built store action. The caller
// must hold the state lock; it is released for the store round-trip.
//
// If forceChannel is not empty, it is set on the action after
// completeStoreAction so a validation-set pin cannot drop the channel.
func sendOneStoreAction(ctx context.Context, st *state.State, action *store.SnapAction, revOpts RevisionOptions, snapst *SnapState, opts Options, includeResources, ignoreValidation bool, forceChannel string) (store.SnapActionResult, error) {
	if err := completeStoreAction(action, revOpts, ignoreValidation); err != nil {
		return store.SnapActionResult{}, err
	}
	if forceChannel != "" {
		action.Channel = forceChannel
	}

	curSnaps, err := currentSnaps(st)
	if err != nil {
		return store.SnapActionResult{}, err
	}

	refreshOpts, err := refreshOptions(st, &store.RefreshOptions{
		IncludeResources: includeResources,
	})
	if err != nil {
		return store.SnapActionResult{}, err
	}

	// Prefer the user that installed the snap, then the user driving this
	// change. A fresh install has no snap user and uses opts.UserID.
	// Refresh drops a user with no store auth, as the first refresh does.
	user, err := userFromUserID(st, snapst.UserID, opts.UserID)
	if err != nil {
		return store.SnapActionResult{}, err
	}
	if action.Action == "refresh" && user != nil && !user.HasStoreAuth() {
		user = nil
	}

	str := Store(st, opts.DeviceCtx)

	st.Unlock()
	results, _, err := str.SnapAction(ctx, curSnaps, []*store.SnapAction{action}, nil, user, refreshOpts)
	st.Lock()

	if err != nil {
		return store.SnapActionResult{}, singleActionResultErr(action.InstanceName, action.Action, err)
	}
	if len(results) != 1 {
		return store.SnapActionResult{}, fmt.Errorf("internal error: expected exactly one result from store, got %d", len(results))
	}
	return results[0], nil
}

// rejectSnapdTrackRedirect requires the store to honour the mapped track.
// A differing track is an error. Same-track or empty RedirectChannel is OK;
// RedirectChannel is then cleared so tracking stays the mapped channel.
func rejectSnapdTrackRedirect(sar *store.SnapActionResult, mapped, action string) error {
	if sar.RedirectChannel == "" {
		return nil
	}

	mappedTrack, err := channelTrack(mapped)
	if err != nil {
		return err
	}
	redirectTrack, err := channelTrack(sar.RedirectChannel)
	if err != nil {
		return fmt.Errorf("cannot parse store redirect channel %q: %v", sar.RedirectChannel, err)
	}
	if mappedTrack != redirectTrack {
		return fmt.Errorf("cannot %s snapd: store redirected from %q to %q", action, mapped, sar.RedirectChannel)
	}

	sar.RedirectChannel = ""
	return nil
}

func storeChannelsEqual(a, b string) bool {
	ca, err1 := channel.Parse(a, "-")
	cb, err2 := channel.Parse(b, "-")
	if err1 != nil || err2 != nil {
		return a == b
	}
	return ca.String() == cb.String()
}

func channelTrack(ch string) (string, error) {
	parsed, err := channel.ParseVerbatim(ch, "-")
	if err != nil {
		return "", err
	}
	if parsed.Track == "" {
		return "latest", nil
	}
	return parsed.Track, nil
}
