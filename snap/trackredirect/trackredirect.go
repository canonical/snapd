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

// Package trackredirect implements snapd track-redirects policy from snap.yaml.
//
// A track-aware snapd consults this package when resolving snapd store
// channels. Track awareness does not imply that snapd carries a populated
// map; maps are added incrementally as LTS branches are onboarded.
//
// UbuntuCoreKey selects the ubuntu-core os-release ID and version from a model.
// Resolve looks up that (or any other) key in snap.Info.TrackRedirects and
// rewrites the channel.
package trackredirect

import (
	"errors"
	"fmt"
	"strconv"

	"github.com/snapcore/snapd/asserts"
	snapchannel "github.com/snapcore/snapd/snap/channel"
)

const (
	// UbuntuCoreID is the os-release ID for Ubuntu Core.
	UbuntuCoreID = "ubuntu-core"
)

// Key identifies a slice of the track-redirects map: ID → version
// (os-release ID and VERSION_ID).
type Key struct {
	ID      string
	Version string
}

var (
	// ErrNotApplicable is returned when a selector does not apply to the system.
	ErrNotApplicable = errors.New("cannot use UC tracks")
	// ErrNotCovered is returned when the key's ID or version is missing
	// from the map. Callers pass through: no channel restriction applies
	// until that version is onboarded.
	ErrNotCovered = errors.New("cannot find track redirects")
	// ErrNoTrack is returned when the version is covered but the input
	// track is neither a map key nor a map value. Store callers refuse.
	ErrNoTrack = errors.New("cannot find track redirect for input track")
)

// UbuntuCoreKey returns the track-redirects key for an Ubuntu Core model.
// It returns ErrNotApplicable for classic, hybrid classic, and UC16.
func UbuntuCoreKey(model *asserts.Model) (Key, error) {
	if model == nil {
		return Key{}, fmt.Errorf("internal error: cannot use nil model")
	}

	if model.Classic() {
		if model.HybridClassic() {
			return Key{}, fmt.Errorf("%w on a hybrid classic system", ErrNotApplicable)
		}
		return Key{}, fmt.Errorf("%w on a classic system", ErrNotApplicable)
	}

	bootBase, err := model.BaseCoreVersion()
	if err != nil {
		return Key{}, fmt.Errorf("internal error: cannot determine boot base: %v", err)
	}
	// UC16 uses the core snap as both base and snapd, so there is no
	// separate snapd snap to apply track policy to.
	if bootBase == 16 {
		return Key{}, fmt.Errorf("%w: unsupported Ubuntu Core 16 model", ErrNotApplicable)
	}
	return Key{ID: UbuntuCoreID, Version: strconv.Itoa(bootBase)}, nil
}

// Resolve applies track-redirects policy to channel using the map slice for
// key. On success it returns the remapped channel with the target track, the
// original risk, and any branch dropped. On failure it returns ("", err).
//
// Policy errors wrap sentinels: ErrNotCovered when key.ID or key.Version
// is missing from the map, and ErrNoTrack when the slice exists but the input
// track is neither a transition key nor a target track. Channel parse
// failures are plain errors. Empty ID or version is an internal error.
//
// Channel is the planned store channel (typically SnapSetup.Channel after
// resolveChannel). Risk-only names are interpreted as the store does: a
// missing track means latest, so "stable" is latest/stable. This function
// does not inherit a tracking track; that merge must already have happened.
func Resolve(key Key, channel string, trackRedirects map[string]map[string]map[string]string) (string, error) {
	if key.ID == "" || key.Version == "" {
		return "", fmt.Errorf("internal error: cannot resolve track redirects with empty key")
	}

	parsed, err := snapchannel.ParseVerbatim(channel, "-")
	if err != nil {
		return "", fmt.Errorf("cannot parse input channel: %v", err)
	}
	inputTrack := parsed.Track
	if inputTrack == "" {
		inputTrack = "latest"
	}

	targetTrack, err := resolveTrack(key, trackRedirects, inputTrack)
	if err != nil {
		return "", err
	}

	parsed.Track = targetTrack
	parsed.Branch = ""
	return parsed.Clean().String(), nil
}

func resolveTrack(key Key, trackRedirects map[string]map[string]map[string]string, inputTrack string) (string, error) {
	byID, ok := trackRedirects[key.ID]
	if !ok {
		return "", fmt.Errorf("%w for %s %s", ErrNotCovered, key.ID, key.Version)
	}
	rules, ok := byID[key.Version]
	if !ok {
		return "", fmt.Errorf("%w for %s %s", ErrNotCovered, key.ID, key.Version)
	}
	targetTrack, found := lookupTrack(rules, inputTrack)
	if !found {
		return "", fmt.Errorf("%w %s for %s %s", ErrNoTrack, inputTrack, key.ID, key.Version)
	}
	return targetTrack, nil
}

// lookupTrack returns the target track for inputTrack. Keys are
// transitions (latest → 18). If inputTrack already matches a target
// (e.g. "18" after a previous jump), it is kept. An explicit key wins,
// so a later onboard can remap onward ("18": "24").
func lookupTrack(rules map[string]string, inputTrack string) (targetTrack string, found bool) {
	if targetTrack, ok := rules[inputTrack]; ok && targetTrack != "" {
		return targetTrack, true
	}
	// Already on a target track (e.g. "18" after a previous jump): keep it.
	for _, target := range rules {
		if target != "" && target == inputTrack {
			return inputTrack, true
		}
	}
	return "", false
}
