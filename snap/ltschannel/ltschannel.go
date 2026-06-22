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

// Package ltschannel implements snapd LTS track policy for Ubuntu Core models.
//
// An LTS-aware snapd consults this package when resolving snapd store
// channels. LTS awareness does not imply the snapd carries an LTS track map;
// maps are added incrementally as LTS branches are onboarded.
//
// SnapdLTSChannel reads the LTS track map from the running snapd, or from a
// candidate snapd snap when one is supplied for inspection.
package ltschannel

import (
	"fmt"

	"github.com/snapcore/snapd/asserts"
	"github.com/snapcore/snapd/snap"
	snapchannel "github.com/snapcore/snapd/snap/channel"
)

var (
	// supportUbuntuCore gates Ubuntu Core models.
	supportUbuntuCore = true
	// supportClassic gates classic (non-hybrid) models.
	supportClassic = false
	// supportHybridClassic gates hybrid classic models (classic with modes).
	supportHybridClassic = false
)

// SnapdLTSChannel applies LTS track policy to channel for model. On success it
// returns the remapped channel with the LTS target track, the original risk, and
// any branch dropped. On failure it returns ("", err). Errors are typed:
// LTSNotAllowedError when the model's system type or boot base is not allowed,
// LTSNoTrackError when the boot base or input track has no LTS mapping, and
// LTSInternalError for nil model, parse failures, or map load failures. When
// candidate is non-nil the map is read from that snapd snap; otherwise from the
// running snapd.
func SnapdLTSChannel(model *asserts.Model, channel string, candidate snap.Container) (string, error) {
	if model == nil {
		return "", &LTSInternalError{Msg: "cannot use nil model"}
	}

	parsed, err := snapchannel.ParseVerbatim(channel, "-")
	if err != nil {
		return "", &LTSInternalError{Msg: fmt.Sprintf("cannot parse input channel: %v", err)}
	}
	inputTrack := parsed.Track
	if inputTrack == "" {
		inputTrack = "latest"
	}

	bootBase, err := systemBootBaseAllowed(model)
	if err != nil {
		return "", err
	}

	var ltsTrack string
	if candidate != nil {
		candidateTrackMap, candidateVersion, err := snap.SnapdLTSTrackMapFromSnapFile(candidate)
		if err != nil {
			return "", &LTSInternalError{Msg: fmt.Sprintf("cannot retrieve LTS track map from candidate snapd version %s: %v", candidateVersion, err)}
		}
		baseTrackMap, ok := candidateTrackMap[bootBase]
		if !ok {
			return "", &LTSNoTrackError{Msg: fmt.Sprintf("no LTS track map for boot base %d from candidate snapd version %s", bootBase, candidateVersion)}
		}
		ltsTrack, ok = baseTrackMap[inputTrack]
		if !ok || ltsTrack == "" {
			return "", &LTSNoTrackError{Msg: fmt.Sprintf("no LTS track for boot base %d for input track %q from candidate snapd version %s", bootBase, inputTrack, candidateVersion)}
		}
	} else {
		thisTrackMap, thisVersion, err := snap.SnapdLTSTrackMapFromThis()
		if err != nil {
			return "", &LTSInternalError{Msg: fmt.Sprintf("cannot retrieve LTS track map from running snapd version %s: %v", thisVersion, err)}
		}
		baseTrackMap, ok := thisTrackMap[bootBase]
		if !ok {
			return "", &LTSNoTrackError{Msg: fmt.Sprintf("no LTS track map for boot base %d from running snapd version %s", bootBase, thisVersion)}
		}
		ltsTrack, ok = baseTrackMap[inputTrack]
		if !ok || ltsTrack == "" {
			return "", &LTSNoTrackError{Msg: fmt.Sprintf("no LTS track for boot base %d for input track %q from running snapd version %s", bootBase, inputTrack, thisVersion)}
		}
	}

	parsed.Track = ltsTrack
	parsed.Branch = ""
	return parsed.Clean().String(), nil
}

// systemBootBaseAllowed returns the boot-base version to consult for LTS policy
// when it applies to the model's system type. It returns an error when the
// system type or boot base is not allowed.
func systemBootBaseAllowed(model *asserts.Model) (int, error) {
	if model.Classic() {
		if model.HybridClassic() {
			if !supportHybridClassic {
				return 0, &LTSNotAllowedError{Msg: "policy does not allow hybrid classic system"}
			}
		} else if !supportClassic {
			return 0, &LTSNotAllowedError{Msg: "policy does not allow classic system"}
		}
		return 0, &LTSNotAllowedError{Msg: "classic boot base not currently supported"}
	}

	if !supportUbuntuCore {
		return 0, &LTSNotAllowedError{Msg: "policy does not allow ubuntu core system"}
	}

	// A model without a "base" header, or with base "core", is UC16-equivalent:
	// the core snap acts as both base and snapd, so there is no separate snapd
	// snap to apply LTS track policy to.
	base := model.Base()
	if base == "" || base == "core" {
		return 0, &LTSNotAllowedError{Msg: "cannot use unsupported Ubuntu Core 16 model"}
	}

	bootBase, err := model.CoreVersion()
	if err != nil {
		return 0, &LTSInternalError{Msg: fmt.Sprintf("cannot determine boot base: %v", err)}
	}
	if bootBase == 16 {
		return 0, &LTSNotAllowedError{Msg: "cannot use unsupported Ubuntu Core 16 model"}
	}
	return bootBase, nil
}
