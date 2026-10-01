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

// Package uctrack implements snapd track policy for Ubuntu Core models.
//
// A track-aware snapd consults this package when resolving snapd store
// channels. Track awareness does not imply that snapd carries a track map;
// maps are added incrementally as LTS branches are onboarded.
package uctrack

import (
	"errors"
	"fmt"
	"strconv"

	"github.com/snapcore/snapd/asserts"
	"github.com/snapcore/snapd/snap"
	snapchannel "github.com/snapcore/snapd/snap/channel"
)

// Errors reported by [Resolve]. They are wrapped, so callers must test them
// with [errors.Is] rather than by comparison.
var (
	// ErrNotApplicable indicates that track policy does not apply.
	// [Resolve] returns it for classic and hybrid classic models, for a boot
	// base that is not a coreXX snap, for Ubuntu Core 16, for an empty
	// tracking channel, for a boot base with no track map, and for a track
	// that the map does not cover. Callers pass the channel through unchanged.
	ErrNotApplicable = errors.New("cannot use Ubuntu Core tracks")
	// ErrRequestedChannelProhibited indicates that the requested track
	// is not the resolved tracking track. Callers fail the operation.
	ErrRequestedChannelProhibited = errors.New("cannot use requested track")
)

// SystemBootBaseApplicable returns the boot base version to consult for
// track policy, as reported by [asserts.Model.BaseCoreVersion]. It returns
// [ErrNotApplicable] for classic, hybrid classic, a boot base that is not a
// coreXX snap, and Ubuntu Core 16. It does not consult a track map. A nil
// model is an internal error.
func SystemBootBaseApplicable(model *asserts.Model) (int, error) {
	if model == nil {
		return 0, errors.New("internal error: cannot use nil model")
	}
	if model.Classic() {
		if model.HybridClassic() {
			return 0, fmt.Errorf("%w on a hybrid classic system", ErrNotApplicable)
		}
		return 0, fmt.Errorf("%w on a classic system", ErrNotApplicable)
	}

	bootBase, err := model.BaseCoreVersion()
	// An error means the model base could not be read as a coreXX snap.
	if err != nil {
		return 0, fmt.Errorf("%w: %q is not a core boot base", ErrNotApplicable, model.Base())
	}
	// UC16 uses the core snap as both base and snapd, so there is no
	// separate snapd snap to apply track policy to.
	if bootBase == 16 {
		return 0, fmt.Errorf("%w: unsupported Ubuntu Core 16 model", ErrNotApplicable)
	}
	return bootBase, nil
}

// Resolve remaps a snapd store channel for model.
// trackingChannel is the channel snapd is tracking, and requestedChannel is the
// channel the caller asked for. An empty trackingChannel returns "" and
// [ErrNotApplicable]. There is nothing to resolve, so the caller keeps its
// own channel.
//
// When trackingChannel resolves, an empty requestedChannel returns that
// channel. A requestedChannel is honored only when its track is already that
// resolved track; the track is copied onto it and is not looked up again.
// The returned channel keeps the risk and branch of the channel it returns:
// the tracking channel when requested is empty, otherwise the requested
// channel. A different track fails with [ErrRequestedChannelProhibited].
//
// A channel without a track means latest, as in the store. tracks is normally
// [snap.Info.UbuntuCoreTracks] of the snapd snap being planned; an empty or
// nil map is valid.
//
// It fails with [ErrNotApplicable] when policy cannot be applied, and with
// [ErrRequestedChannelProhibited] when the requested track is not the
// resolved tracking track.
func Resolve(model *asserts.Model, trackingChannel, requestedChannel string, tracks snap.UbuntuCoreTracks) (string, error) {
	bootBase, err := SystemBootBaseApplicable(model)
	if err != nil {
		return "", err
	}

	if trackingChannel == "" {
		return "", fmt.Errorf("%w: empty tracking channel", ErrNotApplicable)
	}
	normTrackingChannel, err := normalizeChannel(trackingChannel, "tracking")
	if err != nil {
		return "", err
	}

	resolvedTrack, err := resolveUCTrack(tracks, bootBase, normTrackingChannel.Track)
	if err != nil {
		return "", err
	}

	if requestedChannel == "" {
		normTrackingChannel.Track = resolvedTrack
		return normTrackingChannel.Clean().String(), nil
	}

	normRequestedChannel, err := normalizeChannel(requestedChannel, "requested")
	if err != nil {
		return "", err
	}
	if resolvedTrack != normRequestedChannel.Track {
		return "", fmt.Errorf("%w %q: resolved track is %q",
			ErrRequestedChannelProhibited, normRequestedChannel.Track, resolvedTrack)
	}
	normRequestedChannel.Track = resolvedTrack
	return normRequestedChannel.Clean().String(), nil
}

// normalizeChannel parses channel for track policy. An omitted track is
// latest, as in the store. The branch is kept. what names the channel in the
// error.
func normalizeChannel(channel, what string) (snapchannel.Channel, error) {
	parsed, err := snapchannel.ParseVerbatim(channel, "-")
	if err != nil {
		return snapchannel.Channel{}, fmt.Errorf("internal error: cannot parse %s channel: %v", what, err)
	}
	if parsed.Track == "" {
		parsed.Track = "latest"
	}
	return parsed, nil
}

// resolveUCTrack looks up the target track for bootBase and inputTrack in
// tracks. bootBase is the Ubuntu Core version taken from the model, matched
// against the plain number keys of [snap.UbuntuCoreTracks] ("18", "20", ...).
func resolveUCTrack(tracks snap.UbuntuCoreTracks, bootBase int, inputTrack string) (string, error) {
	baseTrackMap, ok := tracks[strconv.Itoa(bootBase)]
	if !ok {
		return "", fmt.Errorf("%w: no track map for boot base %d", ErrNotApplicable, bootBase)
	}
	ucTrack, found := lookupUCTrack(baseTrackMap, inputTrack)
	if !found {
		return "", fmt.Errorf("%w: no track %s for boot base %d", ErrNotApplicable, inputTrack, bootBase)
	}
	return ucTrack, nil
}

// lookupUCTrack returns the target track for inputTrack, and whether one was
// found. Keys describe a transition, such as latest to 18. An input that
// already matches a target, such as "18" after an earlier jump, is kept as
// is. An explicit key wins over that, so a later onboard can remap onward by
// declaring "18": "24".
func lookupUCTrack(baseTrackMap map[string]string, inputTrack string) (string, bool) {
	if ucTrack, ok := baseTrackMap[inputTrack]; ok && ucTrack != "" {
		return ucTrack, true
	}
	for _, target := range baseTrackMap {
		if target != "" && target == inputTrack {
			return inputTrack, true
		}
	}
	return "", false
}
