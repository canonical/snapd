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
	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/snap"
	snapchannel "github.com/snapcore/snapd/snap/channel"
)

// Errors reported by [Resolve]. They are wrapped, so callers must test them
// with [errors.Is] rather than by comparison.
var (
	// ErrNotApplicable indicates that track policy does not apply.
	// [Resolve] returns it for classic and hybrid classic models, for a boot
	// base that is not a coreXX snap, for Ubuntu Core 16, for an empty
	// tracking channel, for a boot base with no track map, and for a tracking
	// track that is neither an input nor a target. Callers pass the channel
	// through unchanged. A requested track outside the map is not this error.
	ErrNotApplicable = errors.New("cannot use Ubuntu Core tracks")
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
// trackingChannel is the channel snapd is tracking. requestedChannel is the
// channel the caller asked for, or empty when there is no request.
//
// The request is considered only after the tracking channel satisfies the
// map. A tracking track that is an input moves to that input's target, keeping
// the tracking risk and dropping the branch. A tracking track that is only a
// target stays, including its risk and branch. A track that is both follows
// the hop instead, because input membership is tested first. That lookup's
// target is not looked up again.
//
// A requested track that is an input is a second map lookup of that input's
// target, not the tracking hop, and does not use the channel carried in from
// the tracking decision. The request supplies the risk and the branch is
// dropped. A requested track that is only a target is used with its risk and
// branch. A requested track that is neither is ignored.
//
// An empty tracking channel, a boot base the map does not cover, and a
// tracking track that is neither an input nor a target return [ErrNotApplicable]
// and an empty channel, so the caller keeps its own channel. An unparseable
// channel is an internal error.
//
// A channel without a track means latest. The returned channel is cleaned, so
// a latest track is omitted. tracks is normally [snap.Info.UbuntuCoreTracks].
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

	pairs, ok := tracks[strconv.Itoa(bootBase)]
	if !ok {
		return "", fmt.Errorf("%w: no track map for boot base %d", ErrNotApplicable, bootBase)
	}

	// The request is considered only after the tracking channel satisfies the
	// map. Input membership is tested before target membership, so a track
	// that is both follows the hop instead.
	trackingTarget, trackingRole := classifyTrack(pairs, normTrackingChannel.Track)
	if trackingRole == trackNeither {
		return "", fmt.Errorf("%w: no track %s for boot base %d", ErrNotApplicable, normTrackingChannel.Track, bootBase)
	}
	carried := normTrackingChannel
	if trackingRole == trackInput {
		carried.Track = trackingTarget
		carried.Branch = ""
	}
	carriedChannel := carried.Clean().String()

	if requestedChannel == "" {
		return carriedChannel, nil
	}
	normRequestedChannel, err := normalizeChannel(requestedChannel, "requested")
	if err != nil {
		return "", err
	}

	// A requested input is a second map lookup of that input's target, not
	// the tracking hop, and does not use the channel carried in from tracking.
	requestedTarget, requestedRole := classifyTrack(pairs, normRequestedChannel.Track)
	switch requestedRole {
	case trackInput:
		normRequestedChannel.Track = requestedTarget
		normRequestedChannel.Branch = ""
		result := normRequestedChannel.Clean().String()
		if trackingRole == trackTargetOnly {
			logger.Noticef("rewrote requested channel %q to %q", requestedChannel, result)
		}
		return result, nil
	case trackTargetOnly:
		return normRequestedChannel.Clean().String(), nil
	default:
		logger.Noticef("ignoring requested channel %q, using %q", requestedChannel, carriedChannel)
		return carriedChannel, nil
	}
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

// trackRole is how a track sits in one boot base's pairs.
type trackRole int

const (
	trackNeither trackRole = iota
	trackInput
	trackTargetOnly
)

// classifyTrack reports whether track is an input, only a target, or neither
// for one boot base. Input membership is tested before target membership. A
// track that is both follows the hop instead, because input membership is
// tested first. The returned target is that one hop: an input's map value is
// not looked up again. An empty map value is not an input or a target.
func classifyTrack(pairs map[string]string, track string) (target string, role trackRole) {
	if mapped, ok := pairs[track]; ok && mapped != "" {
		return mapped, trackInput
	}
	for _, mapped := range pairs {
		if mapped != "" && mapped == track {
			return track, trackTargetOnly
		}
	}
	return "", trackNeither
}
