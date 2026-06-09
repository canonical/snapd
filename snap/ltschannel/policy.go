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
	"strings"

	"github.com/snapcore/snapd/asserts"
)

// snapdLTSTrackMap maps each supported boot base to an allow-list of snapd
// input tracks and the LTS track they resolve to. The lookup key is the input
// track (empty input is treated as "latest"); the value is the LTS target
// track. Risk is preserved from the input; branches are dropped.
//
// Tracks not present for a managed boot base are rejected. Boot bases not
// listed are unmanaged and channels are passed through unchanged.
//
// Boot bases are onboarded gradually. UC16 is unsupported (no boot base).
//
// Example of a mature map:
//
//	{
//	    18: {"latest": "18", "fips-updates": "18-fips", "18": "18", "18-fips": "18-fips"},
//	    20: {"latest": "20", "fips-updates": "20-fips", "20": "20", "20-fips": "20-fips"},
//	    22: {"latest": "22", "fips-updates": "22-fips", "22": "22", "22-fips": "22-fips"},
//	}
var snapdLTSTrackMap = map[int]map[string]string{
	// Boot bases are onboarded gradually. Production map is intentionally
	// empty until the first UC version reaches its LTS transition. Example:
	// 18: {"latest": "18", "fips-updates": "18-fips", "18": "18", "18-fips": "18-fips"},
}

var (
	// supportUbuntuCore gates Ubuntu Core models.
	supportUbuntuCore = true
	// supportClassic gates classic (non-hybrid) models.
	supportClassic = false
	// supportHybridClassic gates hybrid classic models (classic with modes).
	supportHybridClassic = false
)

// snapdLTSTracksForModel returns the LTS track allow-list that applies to
// the model. applies=false means LTS policy does not apply (device kind out
// of scope per the support* flags, model has no core base, or boot base not
// yet onboarded) and callers should pass channels through unchanged. Returns
// an error for explicitly unsupported models (UC16).
func snapdLTSTracksForModel(model *asserts.Model) (tracks map[string]string, applies bool, err error) {
	// device-kind scope gate
	if model.Classic() {
		if model.HybridClassic() {
			if !supportHybridClassic {
				return nil, false, nil
			}
		} else if !supportClassic {
			return nil, false, nil
		}
	} else if !supportUbuntuCore {
		return nil, false, nil
	}

	// boot-base scope gate
	bootBase, err := model.CoreVersion()
	if err != nil {
		// Model base is not a core snap (e.g. a classic model with a
		// non-core base). Unmanaged.
		return nil, false, nil
	}
	if bootBase == 16 {
		// UC16 is explicitly unsupported by LTS snapd channel policy.
		return nil, false, errors.New("cannot use unsupported Ubuntu Core 16 model")
	}
	tracks, managed := snapdLTSTrackMap[bootBase]
	if !managed {
		// Boot base not yet onboarded.
		return nil, false, nil
	}
	return tracks, true, nil
}

// MockSnapdLTSDeviceKindScope replaces the device-kind scope flags consulted
// by snapdLTSTracksForModel for tests.
func MockSnapdLTSDeviceKindScope(supportUC, supportCl, supportHybrid bool) (restore func()) {
	restoreUC, restoreCl, restoreHybrid := supportUbuntuCore, supportClassic, supportHybridClassic
	supportUbuntuCore = supportUC
	supportClassic = supportCl
	supportHybridClassic = supportHybrid
	return func() {
		supportUbuntuCore = restoreUC
		supportClassic = restoreCl
		supportHybridClassic = restoreHybrid
	}
}

// MockSnapdLTSTrackMap replaces snapdLTSTrackMap for tests. For each boot
// base, the first track is the default target for the "latest" input track
// and is also accepted as an identity input; every additional track is
// accepted as itself; any "-fips" track also installs a "fips-updates" alias
// mapping to it.
func MockSnapdLTSTrackMap(tracks map[int][]string) (restore func()) {
	restoreMap := snapdLTSTrackMap
	mock := make(map[int]map[string]string, len(tracks))
	for bootBase, ltsTracks := range tracks {
		if len(ltsTracks) == 0 {
			continue
		}
		rules := map[string]string{
			"latest": ltsTracks[0],
		}
		for _, track := range ltsTracks {
			rules[track] = track
			if strings.HasSuffix(track, "-fips") {
				rules["fips-updates"] = track
			}
		}
		mock[bootBase] = rules
	}
	snapdLTSTrackMap = mock
	return func() {
		snapdLTSTrackMap = restoreMap
	}
}
