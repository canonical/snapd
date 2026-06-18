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

package ltschannel

import (
	"errors"
	"fmt"

	"github.com/snapcore/snapd/asserts"
	snapchannel "github.com/snapcore/snapd/snap/channel"
)

// ErrNoLTSTrack is the sentinel matched by errors.Is when SnapdLTSChannel
// rejects an input channel whose track is not in the LTS allow-list for the
// model's managed boot base. It is the LTS-policy analogue of
// channel.ErrPinnedTrackSwitch. Callers wanting the offending track name
// should use errors.As with *NoLTSTrackError.
var ErrNoLTSTrack = errors.New("no LTS track")

// NoLTSTrackError is returned by SnapdLTSChannel when the input track has no
// LTS mapping for the model's managed boot base. errors.Is matches
// ErrNoLTSTrack.
type NoLTSTrackError struct{ Track string }

func (e *NoLTSTrackError) Error() string {
	return fmt.Sprintf("cannot resolve LTS channel for track %q", e.Track)
}

func (e *NoLTSTrackError) Is(target error) bool { return target == ErrNoLTSTrack }

// SnapdLTSChannel returns the snapd channel to use for the given model derived
// from the given input channel, applying LTS-track policy using the LTS track
// map from the currently running snapd's info file.
//
// Behaviour:
//   - If LTS policy does not apply to the model (device kind out of scope,
//     non-core base, or boot base not yet onboarded), the input channel is
//     returned unchanged.
//   - If LTS policy applies and the input track is in the model's LTS
//     allow-list, the channel is rewritten: the track is swapped for the
//     LTS target track, the risk is preserved and any branch is dropped.
//     The input track "" is normalised to "latest" before lookup.
//   - If LTS policy applies and the input track is not in the allow-list,
//     a *NoLTSTrackError is returned (matchable as ErrNoLTSTrack via
//     errors.Is).
//
// Other errors: nil model, unparseable input channel, explicitly unsupported
// models (UC16), and failures reading the running snapd info file all return
// errors that are not ErrNoLTSTrack.
func SnapdLTSChannel(model *asserts.Model, channel string) (string, error) {
	trackMap, err := snapdLTSTrackMapLoader()
	if err != nil {
		return "", err
	}
	return SnapdLTSChannelWithTrackMap(model, channel, trackMap)
}

// SnapdLTSChannelWithTrackMap is like SnapdLTSChannel but uses the provided
// LTS track map instead of loading from the running snapd. This is used when
// inspecting a candidate snapd snap after download.
func SnapdLTSChannelWithTrackMap(model *asserts.Model, channel string, trackMap map[int]map[string]string) (string, error) {
	if model == nil {
		return "", errors.New("cannot use nil model")
	}
	tracks, applies, err := snapdLTSTracksForModelWithMap(model, trackMap)
	if err != nil {
		return "", err
	}
	if !applies {
		return channel, nil
	}

	parsed, err := snapchannel.ParseVerbatim(channel, "-")
	if err != nil {
		return "", fmt.Errorf("cannot parse input channel: %v", err)
	}
	inputTrack := parsed.Track
	if inputTrack == "" {
		inputTrack = "latest"
	}
	target, ok := tracks[inputTrack]
	if !ok {
		return "", &NoLTSTrackError{Track: inputTrack}
	}
	parsed.Track = target
	parsed.Branch = ""
	return parsed.Clean().String(), nil
}
