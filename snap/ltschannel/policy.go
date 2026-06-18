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
	"github.com/snapcore/snapd/snap"
)

var (
	// supportUbuntuCore gates Ubuntu Core models.
	supportUbuntuCore = true
	// supportClassic gates classic (non-hybrid) models.
	supportClassic = false
	// supportHybridClassic gates hybrid classic models (classic with modes).
	supportHybridClassic = false
)

var (
	runningSnapdLTSTracks    map[int]map[string]string
	runningSnapdLTSTracksErr error
)

func init() {
	runningSnapdLTSTracks, runningSnapdLTSTracksErr = snap.SnapdLTSTracksFromRunningSnapd()
}

func snapdLTSTrackMapFromRunningSnapd() (map[int]map[string]string, error) {
	return runningSnapdLTSTracks, runningSnapdLTSTracksErr
}

var snapdLTSTrackMapLoader = snapdLTSTrackMapFromRunningSnapd

// snapdLTSTracksForModel returns the LTS track allow-list that applies to
// the model, loaded from the running snapd's info file. applies=false means
// LTS policy does not apply (device kind out of scope per the support* flags,
// model has no core base, or boot base not yet onboarded) and callers should
// pass channels through unchanged. Returns an error for explicitly unsupported
// models (UC16) or when the running snapd info file cannot be read.
func snapdLTSTracksForModel(model *asserts.Model) (tracks map[string]string, applies bool, err error) {
	trackMap, err := snapdLTSTrackMapLoader()
	if err != nil {
		return nil, false, err
	}
	return snapdLTSTracksForModelWithMap(model, trackMap)
}

// snapdLTSTracksForModelWithMap is like snapdLTSTracksForModel but uses the
// provided track map instead of loading from the running snapd.
func snapdLTSTracksForModelWithMap(model *asserts.Model, trackMap map[int]map[string]string) (tracks map[string]string, applies bool, err error) {
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
	tracks, managed := trackMap[bootBase]
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

// snapdLTSTrackMapFromTracks builds the input-track → LTS-target-track map
// used by tests and MockSnapdLTSTrackMap. For each boot base, the first track
// is the default target for the "latest" input track and is also accepted as
// an identity input; every additional track is accepted as itself; any "-fips"
// track also installs a "fips-updates" alias mapping to it.
func snapdLTSTrackMapFromTracks(tracks map[int][]string) map[int]map[string]string {
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
	return mock
}

// MockSnapdLTSTrackMap replaces the running snapd LTS track map loader for
// tests.
func MockSnapdLTSTrackMap(tracks map[int][]string) (restore func()) {
	restoreLoader := snapdLTSTrackMapLoader
	mock := snapdLTSTrackMapFromTracks(tracks)
	snapdLTSTrackMapLoader = func() (map[int]map[string]string, error) {
		return mock, nil
	}
	return func() {
		snapdLTSTrackMapLoader = restoreLoader
	}
}
