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
// Resolve reads the LTS track map from this process, or from a
// candidate snapd snap when one is supplied for inspection.
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

var (
	// supportUbuntuCore gates Ubuntu Core models.
	supportUbuntuCore = true
)

// Resolve applies LTS track policy to channel for model. On success it
// returns the remapped channel with the LTS target track, the original risk, and
// any branch dropped. On failure it returns ("", err). Policy errors wrap
// sentinels: ErrNotAllowed when the model's system type or boot base is not
// allowed, ErrNoTrack when the boot base is managed but the input track is
// neither a transition key nor an LTS target, and ErrBootBaseNotManaged when the
// boot base has no map entry. Channel parse and map-load failures are plain
// errors. Programming errors (nil model, undetermined boot base) are prefixed
// with "internal error:". When candidate is non-nil the map is read from that
// snapd snap; otherwise from this process.
//
// Channel is the planned store channel (typically SnapSetup.Channel after
// resolveChannel). Risk-only names are interpreted as the store does: a
// missing track means latest, so "stable" is latest/stable. This function
// does not inherit a tracking track; that merge must already have happened.
func Resolve(model *asserts.Model, channel string, candidate snap.Container) (string, error) {
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

	var (
		trackMap map[int]map[string]string
		version  string
		source   string
	)
	if candidate != nil {
		source = "candidate"
		trackMap, version, err = snap.SnapdLTSTrackMapFromSnapFile(candidate)
	} else {
		source = "this"
		trackMap, version, err = snap.SnapdLTSTrackMapFromThis()
	}
	if err != nil {
		return "", fmt.Errorf("cannot retrieve LTS track map from %s snapd version %s: %v", source, version, err)
	}
	ltsTrack, err := resolveLTSTrack(trackMap, version, source, bootBase, inputTrack)
	if err != nil {
		return "", err
	}

	parsed.Track = ltsTrack
	parsed.Branch = ""
	return parsed.Clean().String(), nil
}

// resolveLTSTrack looks up the LTS target for bootBase and inputTrack in
// trackMap. source labels the map origin in errors ("this" or "candidate").
func resolveLTSTrack(trackMap map[int]map[string]string, version, source string, bootBase int, inputTrack string) (string, error) {
	baseTrackMap, ok := trackMap[bootBase]
	if !ok {
		return "", fmt.Errorf("%w %d from %s snapd version %s", ErrBootBaseNotManaged, bootBase, source, version)
	}
	ltsTrack, ok := lookupLTSTrack(baseTrackMap, inputTrack)
	if !ok {
		return "", fmt.Errorf("%w %s for boot base %d from %s snapd version %s", ErrNoTrack, inputTrack, bootBase, source, version)
	}
	return ltsTrack, nil
}

// lookupLTSTrack returns the LTS target for inputTrack. Keys are transitions
// (latest → 18). If inputTrack is not a key but equals any map value, the
// switch already happened and that track is kept. An explicit key wins over
// implicit identity, so a later onboard can remap an LTS track onward.
func lookupLTSTrack(baseTrackMap map[string]string, inputTrack string) (string, bool) {
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

	if !supportUbuntuCore {
		return 0, fmt.Errorf("%w on ubuntu core system", ErrNotAllowed)
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
