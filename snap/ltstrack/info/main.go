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

// info produces the SNAPD_LTS_TRACKS line for the snapd snap's info file
// (/usr/lib/snapd/info). Distro packages do not include this key. The map is
// the source of truth for the current snapd (snap.SnapdLTSTrackMapFromCurrentSnapd) and
// for a candidate snapd snap (snap.SnapdLTSTrackMapFromSnapFile).
//
// The map is intentionally empty in master / latest snapd until the first UC
// version reaches LTS. Onboarding a UC version is a one-line edit here;
// the change is backported wholesale to release/lts/* so LTS-branch snapd
// applies the same policy.
//
// Shape: snapdLTSTracks[bootBase][inputTrack] = LTSTargetTrack
//
// Input tracks and LTS targets must be track-only names (e.g. "latest",
// "18", "18-fips"), never a risk or full channel such as "18/stable".
//
// Entries are transitions only (latest → 18). If the input track already
// equals any output of that boot-base map, Resolve keeps it (implicit
// identity). A later onboard can remap an LTS track onward by adding an
// explicit key ("18": "24"); that wins because keys are checked first.
//
// Example for a hypothetical onboarded UC18:
//
//	snapdLTSTracks = map[int]map[string]string{
//	    18: {
//	        "latest":       "18",
//	        "fips-updates": "18-fips",
//	    },
//	}
package main

import (
	"encoding/json"
	"fmt"
	"os"

	snapchannel "github.com/snapcore/snapd/snap/channel"
)

// snapdLTSTracks is the LTS track map this snapd build carries. Empty by
// design until a UC version is onboarded.
var snapdLTSTracks = map[int]map[string]string{}

// renderInfoLine returns the single-line SNAPD_LTS_TRACKS entry for the snapd
// info file. The JSON value is single-quoted, matching the format of
// SNAPD_ASSERTS_FORMATS produced by asserts/info. Input tracks and LTS
// targets must be track-only channel names (no risk or branch).
func renderInfoLine(tracks map[int]map[string]string) (string, error) {
	if err := checkLTSTracks(tracks); err != nil {
		return "", err
	}
	b, err := json.Marshal(tracks)
	if err != nil {
		return "", fmt.Errorf("cannot json marshal snapd LTS tracks: %v", err)
	}
	return fmt.Sprintf("SNAPD_LTS_TRACKS='%s'", b), nil
}

func checkLTSTracks(tracks map[int]map[string]string) error {
	for bootBase, rules := range tracks {
		for input, target := range rules {
			if !snapchannel.IsVerbatimTrackOnly(input) {
				return fmt.Errorf("cannot encode snapd LTS tracks: input track %q for boot base %d is not a track-only channel", input, bootBase)
			}
			if !snapchannel.IsVerbatimTrackOnly(target) {
				return fmt.Errorf("cannot encode snapd LTS tracks: LTS target %q for boot base %d is not a track-only channel", target, bootBase)
			}
		}
	}
	return nil
}

func main() {
	line, err := renderInfoLine(snapdLTSTracks)
	if err != nil {
		fmt.Fprintf(os.Stderr, "%v\n", err)
		os.Exit(1)
	}
	fmt.Println(line)
}
