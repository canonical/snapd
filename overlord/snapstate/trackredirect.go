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

	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/snap/channel"
	"github.com/snapcore/snapd/snap/naming"
	"github.com/snapcore/snapd/snap/trackredirect"
	"github.com/snapcore/snapd/store"
)

// maybeRedirectSnapdTrack applies track-redirects policy after the first
// store round-trip, which supplies snap.yaml's track-redirects map. When
// the channel remaps, a second SnapAction fetches the revision to install,
// refresh, or download.
//
// Applies to type: snapd named "snapd". Path/seed/try never call this.
// Classic, hybrid, UC16, and an unmapped boot base pass through.
// ErrNoTrack is refused once the boot base has a map.
//
// A pinned or requested revision is queried on the mapped track, not
// taken from the first result and relabelled. localOnly means a refresh
// found no update on the mapped track: keep the installed revision and
// switch tracking.
func maybeRedirectSnapdTrack(ctx context.Context, st *state.State, sar store.SnapActionResult, revOpts *RevisionOptions, snapst *SnapState, opts Options, action string) (store.SnapActionResult, bool, error) {
	if sar.Info == nil || sar.Info.Type() != snap.TypeSnapd || sar.InstanceName() != "snapd" {
		return sar, false, nil
	}

	deviceCtx := opts.DeviceCtx
	if deviceCtx == nil {
		var err error
		deviceCtx, err = DeviceCtx(st, nil, nil)
		if err != nil {
			return store.SnapActionResult{}, false, err
		}
	}

	inputChannel := revOpts.Channel
	if inputChannel == "" {
		if snapst.TrackingChannel != "" {
			inputChannel = snapst.TrackingChannel
		} else {
			inputChannel = "stable"
		}
	}

	key, err := trackredirect.UbuntuCoreKey(deviceCtx.Model())
	if err != nil {
		if errors.Is(err, trackredirect.ErrNotApplicable) {
			return sar, false, nil
		}
		return store.SnapActionResult{}, false, err
	}

	mapped, err := trackredirect.Resolve(key, inputChannel, sar.Info.TrackRedirects)
	if err != nil {
		if errors.Is(err, trackredirect.ErrNotCovered) {
			return sar, false, nil
		}
		if errors.Is(err, trackredirect.ErrNoTrack) {
			return store.SnapActionResult{}, false, fmt.Errorf("cannot %s snapd: channel %q is not a UC track for boot base %s", action, inputChannel, key.Version)
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
		logger.Noticef("remapping snapd from %q to %q to follow track-redirects", inputChannel, mapped)
	}
	revOpts.Channel = mapped

	sa := &store.SnapAction{
		Action:       action,
		InstanceName: sar.InstanceName(),
	}
	if action == "refresh" {
		sa.SnapID = sar.SnapID
		if sa.SnapID == "" {
			if si := snapst.CurrentSideInfo(); si != nil {
				sa.SnapID = si.SnapID
			}
		}
		if sa.SnapID == "" {
			return store.SnapActionResult{}, false, errors.New("internal error: cannot refresh snapd onto a UC track without a snap id")
		}
	}

	// completeStoreAction clears Channel when a validation set pins a
	// revision; force the mapped channel so the store must serve it there.
	newSAR, err := sendOneStoreAction(ctx, st, sa, *revOpts, opts, len(sar.Resources) > 0, mapped)
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
