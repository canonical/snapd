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
	// ErrNotApplicable indicates that track policy does not apply to the
	// model at all, because of its system type or its boot base.
	ErrNotApplicable = errors.New("cannot use Ubuntu Core tracks")
	// ErrBootBaseNotCovered indicates that the model's boot base has no
	// entry in the map yet. Callers pass the channel through unchanged: no
	// restriction applies until the boot base is onboarded.
	ErrBootBaseNotCovered = errors.New("cannot find Ubuntu Core track map for boot base")
	// ErrNoTrack indicates that the boot base is covered but the input
	// track is neither a key nor a target of its map. Callers pass the
	// channel through unchanged.
	ErrNoTrack = errors.New("cannot find Ubuntu Core track")
)

// Resolve remaps the planned store channel for model, keeping its risk and
// dropping any branch. A channel without a track means latest, as in the
// store. tracks is normally [snap.Info.UbuntuCoreTracks] of the snapd snap being
// planned; an empty or nil map is valid.
//
// It fails with [ErrNotApplicable], [ErrBootBaseNotCovered] or [ErrNoTrack]
// when policy cannot be applied.
func Resolve(model *asserts.Model, channel string, tracks snap.UbuntuCoreTracks) (string, error) {
	if model == nil {
		return "", errors.New("internal error: cannot use nil model")
	}

	parsed, err := snapchannel.ParseVerbatim(channel, "-")
	if err != nil {
		return "", fmt.Errorf("cannot parse input channel: %v", err)
	}
	inputTrack := parsed.Track
	if inputTrack == "" {
		inputTrack = "latest"
	}

	bootBase, err := systemBootBaseApplicable(model)
	if err != nil {
		return "", err
	}

	ucTrack, err := resolveUCTrack(tracks, bootBase, inputTrack)
	if err != nil {
		return "", err
	}

	parsed.Track = ucTrack
	parsed.Branch = ""
	return parsed.Clean().String(), nil
}

// resolveUCTrack looks up the target track for bootBase and inputTrack in
// tracks. bootBase is the Ubuntu Core version taken from the model, matched
// against the plain number keys of [snap.UbuntuCoreTracks] ("18", "20", ...).
func resolveUCTrack(tracks snap.UbuntuCoreTracks, bootBase int, inputTrack string) (string, error) {
	baseTrackMap, ok := tracks[strconv.Itoa(bootBase)]
	if !ok {
		return "", fmt.Errorf("%w %d", ErrBootBaseNotCovered, bootBase)
	}
	ucTrack, found := lookupUCTrack(baseTrackMap, inputTrack)
	if !found {
		return "", fmt.Errorf("%w %s for boot base %d", ErrNoTrack, inputTrack, bootBase)
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
	// Already on a target track (e.g. "18" after a previous jump): keep it.
	for _, target := range baseTrackMap {
		if target != "" && target == inputTrack {
			return inputTrack, true
		}
	}
	return "", false
}

// systemBootBaseApplicable returns the boot base version to consult for
// track policy, as reported by [asserts.Model.BaseCoreVersion]. It fails with
// [ErrNotApplicable] when the model's system type or boot base puts it out of
// scope.
func systemBootBaseApplicable(model *asserts.Model) (int, error) {
	if model.Classic() {
		if model.HybridClassic() {
			return 0, fmt.Errorf("%w on a hybrid classic system", ErrNotApplicable)
		}
		return 0, fmt.Errorf("%w on a classic system", ErrNotApplicable)
	}

	bootBase, err := model.BaseCoreVersion()
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
