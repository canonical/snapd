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
// channels. Track awareness does not imply that snapd carries a UC track map;
// maps are added incrementally as LTS branches are onboarded.
//
// Resolve reads the UC track map from the running snapd, or from
// candidateSnapd when a snapd install/refresh candidate is supplied.
package uctrack

import (
	"errors"
	"fmt"

	"github.com/snapcore/snapd/asserts"
	"github.com/snapcore/snapd/snap"
	snapchannel "github.com/snapcore/snapd/snap/channel"
)

var (
	// ErrNotApplicable is returned when track policy does not apply to the model.
	ErrNotApplicable = errors.New("cannot use UC tracks")
	// ErrBootBaseNotCovered is returned when the model's boot base has no UC
	// track map yet. Callers pass through: no channel restriction applies
	// until the boot base is onboarded.
	ErrBootBaseNotCovered = errors.New("cannot find UC track map for boot base")
	// ErrNoTrack is returned when the boot base is covered but the input
	// track is neither a map key nor a map value. Callers pass through.
	ErrNoTrack = errors.New("cannot find UC track for input track")
)

// Resolve applies track policy to channel for model. On success it
// returns the remapped channel with the target track, the original risk, and
// any branch dropped. On failure it returns ("", err). Policy errors wrap
// sentinels: ErrNotApplicable when track policy does not apply to the model's
// system type or boot base, ErrNoTrack when the boot base is covered but the
// input track is neither a transition key nor a target track, and
// ErrBootBaseNotCovered when the boot base has no map entry. Channel parse and
// map-load failures are plain errors. Programming errors (nil model,
// undetermined boot base) are prefixed with "internal error:".
//
// candidateSnapd is the snapd snap being installed or refreshed.
// It is not a store channel risk. If nil, the map is read from the
// running snapd.
//
// Channel is the planned store channel (typically SnapSetup.Channel after
// resolveChannel). Risk-only names are interpreted as the store does: a
// missing track means latest, so "stable" is latest/stable. This function
// does not inherit a tracking track; that merge must already have happened.
func Resolve(model *asserts.Model, channel string, candidateSnapd snap.Container) (string, error) {
	if model == nil {
		return "", fmt.Errorf("internal error: cannot use nil model")
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

	trackMap, version, origin, err := loadUCTrackMap(candidateSnapd)
	if err != nil {
		return "", fmt.Errorf("cannot retrieve UC track map from %s %s: %v", origin, version, err)
	}
	ucTrack, err := resolveUCTrack(trackMap, version, origin, bootBase, inputTrack)
	if err != nil {
		return "", err
	}

	parsed.Track = ucTrack
	parsed.Branch = ""
	return parsed.Clean().String(), nil
}

var snapdUCTrackMapFromCurrentSnapd = snap.SnapdUCTrackMapFromCurrentSnapd

func loadUCTrackMap(candidateSnapd snap.Container) (trackMap map[int]map[string]string, version, origin string, err error) {
	if candidateSnapd != nil {
		trackMap, version, err = snap.SnapdUCTrackMapFromSnapFile(candidateSnapd)
		return trackMap, version, "candidate snapd snap", err
	}
	trackMap, version, err = snapdUCTrackMapFromCurrentSnapd()
	return trackMap, version, "running snapd", err
}

// resolveUCTrack looks up the target track for bootBase and inputTrack in
// trackMap. origin labels the map source in errors ("running snapd" or
// "candidate snapd snap").
func resolveUCTrack(trackMap map[int]map[string]string, version, origin string, bootBase int, inputTrack string) (string, error) {
	baseTrackMap, ok := trackMap[bootBase]
	if !ok {
		return "", fmt.Errorf("%w %d from %s %s", ErrBootBaseNotCovered, bootBase, origin, version)
	}
	ucTrack, found := lookupUCTrack(baseTrackMap, inputTrack)
	if !found {
		return "", fmt.Errorf("%w %s for boot base %d from %s %s", ErrNoTrack, inputTrack, bootBase, origin, version)
	}
	return ucTrack, nil
}

// lookupUCTrack returns the target track for inputTrack. Keys are
// transitions (latest → 18). If inputTrack already matches a target
// (e.g. "18" after a previous jump), it is kept. An explicit key wins,
// so a later onboard can remap onward ("18": "24").
func lookupUCTrack(baseTrackMap map[string]string, inputTrack string) (ucTrack string, found bool) {
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

// systemBootBaseApplicable returns the boot-base version to consult for track
// policy when it applies to the model's system type. It returns an error when
// the system type or boot base is not applicable.
func systemBootBaseApplicable(model *asserts.Model) (int, error) {
	if model.Classic() {
		if model.HybridClassic() {
			return 0, fmt.Errorf("%w on a hybrid classic system", ErrNotApplicable)
		}
		return 0, fmt.Errorf("%w on a classic system", ErrNotApplicable)
	}

	bootBase, err := model.BaseCoreVersion()
	if err != nil {
		return 0, fmt.Errorf("internal error: cannot determine boot base: %v", err)
	}
	// UC16 uses the core snap as both base and snapd, so there is no
	// separate snapd snap to apply track policy to.
	if bootBase == 16 {
		return 0, fmt.Errorf("%w: unsupported Ubuntu Core 16 model", ErrNotApplicable)
	}
	return bootBase, nil
}
