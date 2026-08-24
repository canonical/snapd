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

// Package ltstrack implements snapd LTS track policy for Ubuntu Core models.
//
// An LTS-aware snapd consults this package when resolving snapd store
// channels. LTS awareness does not imply that snapd carries an LTS track map;
// maps are added incrementally as LTS branches are onboarded.
//
// Resolve reads the LTS track map from the running snapd, or from
// candidateSnapd when a snapd install/refresh candidate is supplied.
package ltstrack

import (
	"errors"
	"fmt"

	"github.com/snapcore/snapd/asserts"
	"github.com/snapcore/snapd/snap"
	snapchannel "github.com/snapcore/snapd/snap/channel"
)

var (
	// ErrNotAllowed is returned when LTS policy does not apply to the model.
	ErrNotAllowed = errors.New("cannot use LTS tracks")
	// ErrBootBaseNotManaged is returned when the model's boot base has no LTS
	// mapping yet. Callers pass through: no channel restriction applies
	// until the boot base is onboarded.
	ErrBootBaseNotManaged = errors.New("cannot find LTS track map for boot base")
	// ErrNoTrack is returned when the boot base is managed but the input
	// track is neither a map key nor a map value. Callers pass through.
	ErrNoTrack = errors.New("cannot find LTS track for input track")
)

// Resolve applies LTS track policy to channel for model. On success it
// returns the remapped channel with the LTS target track, the original risk, and
// any branch dropped. On failure it returns ("", err). Policy errors wrap
// sentinels: ErrNotAllowed when the model's system type or boot base is not
// allowed, ErrNoTrack when the boot base is managed but the input track is
// neither a transition key nor an LTS target, and ErrBootBaseNotManaged when the
// boot base has no map entry. Channel parse and map-load failures are plain
// errors. Programming errors (nil model, undetermined boot base) are prefixed
// with "internal error:".
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

	bootBase, err := systemBootBaseAllowed(model)
	if err != nil {
		return "", err
	}

	trackMap, version, origin, err := loadLTSTrackMap(candidateSnapd)
	if err != nil {
		return "", fmt.Errorf("cannot retrieve LTS track map from %s %s: %v", origin, version, err)
	}
	ltsTrack, err := resolveLTSTrack(trackMap, version, origin, bootBase, inputTrack)
	if err != nil {
		return "", err
	}

	parsed.Track = ltsTrack
	parsed.Branch = ""
	return parsed.Clean().String(), nil
}

func loadLTSTrackMap(candidateSnapd snap.Container) (trackMap map[int]map[string]string, version, origin string, err error) {
	if candidateSnapd != nil {
		trackMap, version, err = snap.SnapdLTSTrackMapFromSnapFile(candidateSnapd)
		return trackMap, version, "candidate snapd snap", err
	}
	trackMap, version, err = snap.SnapdLTSTrackMapFromThis()
	return trackMap, version, "running snapd", err
}

// resolveLTSTrack looks up the LTS target for bootBase and inputTrack in
// trackMap. origin labels the map source in errors ("running snapd" or
// "candidate snapd snap").
func resolveLTSTrack(trackMap map[int]map[string]string, version, origin string, bootBase int, inputTrack string) (string, error) {
	baseTrackMap, ok := trackMap[bootBase]
	if !ok {
		return "", fmt.Errorf("%w %d from %s %s", ErrBootBaseNotManaged, bootBase, origin, version)
	}
	ltsTrack, found := lookupLTSTrack(baseTrackMap, inputTrack)
	if !found {
		return "", fmt.Errorf("%w %s for boot base %d from %s %s", ErrNoTrack, inputTrack, bootBase, origin, version)
	}
	return ltsTrack, nil
}

// lookupLTSTrack returns the LTS target for inputTrack. Keys are
// transitions (latest → 18). If inputTrack already matches a target
// (e.g. "18" after a previous jump), it is kept. An explicit key wins,
// so a later onboard can remap onward ("18": "24").
func lookupLTSTrack(baseTrackMap map[string]string, inputTrack string) (ltsTrack string, found bool) {
	if ltsTrack, ok := baseTrackMap[inputTrack]; ok && ltsTrack != "" {
		return ltsTrack, true
	}
	for _, target := range baseTrackMap {
		if target != "" && target == inputTrack {
			return inputTrack, true
		}
	}
	return "", false
}

// systemBootBaseAllowed returns the boot-base version to consult for LTS policy
// when it applies to the model's system type. It returns an error when the
// system type or boot base is not allowed.
func systemBootBaseAllowed(model *asserts.Model) (int, error) {
	if model.Classic() {
		if model.HybridClassic() {
			return 0, fmt.Errorf("%w on a hybrid classic system", ErrNotAllowed)
		}
		return 0, fmt.Errorf("%w on a classic system", ErrNotAllowed)
	}

	// A model without a "base" header, or with base "core", is UC16-equivalent:
	// the core snap acts as both base and snapd, so there is no separate snapd
	// snap to apply LTS track policy to.
	base := model.Base()
	if base == "" || base == "core" {
		return 0, fmt.Errorf("%w: unsupported Ubuntu Core 16 model", ErrNotAllowed)
	}

	bootBase, err := model.CoreVersion()
	if err != nil {
		return 0, fmt.Errorf("internal error: cannot determine boot base: %v", err)
	}
	if bootBase == 16 {
		return 0, fmt.Errorf("%w: unsupported Ubuntu Core 16 model", ErrNotAllowed)
	}
	return bootBase, nil
}
