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

// info produces the SNAPD_LTS_TRACKS line for /usr/lib/snapd/info, capturing
// the LTS track policy this snapd carries. The map is the source of truth for
// the running snapd's view (read back at runtime from the info file by
// snap.SnapdLTSTrackMapFromThis) and for the candidate-snapd view (read from
// the squashfs at /usr/lib/snapd/info by snap.SnapdLTSTrackMapFromSnapFile).
//
// The map is intentionally empty in master / latest snapd until the first UC
// version reaches LTS. Onboarding a UC version is a one-line edit here;
// the change is backported wholesale to release/lts/* so LTS-branch snapd
// applies the same policy.
//
// Shape: snapdLTSTracks[bootBase][inputTrack] = LTSTargetTrack
//
// Example for a hypothetical onboarded UC18:
//
//	snapdLTSTracks = map[int]map[string]string{
//	    18: {
//	        "latest":       "18",
//	        "fips-updates": "18-fips",
//	        "18":           "18",
//	        "18-fips":      "18-fips",
//	    },
//	}
package main

import (
	"encoding/json"
	"fmt"
)

// snapdLTSTracks is the LTS track map this snapd build carries. Empty by
// design until a UC version is onboarded.
var snapdLTSTracks = map[int]map[string]string{}

// renderInfoLine returns the single-line SNAPD_LTS_TRACKS entry for
// /usr/lib/snapd/info. The JSON value is single-quoted, matching the format
// of SNAPD_ASSERTS_FORMATS produced by asserts/info.
func renderInfoLine(tracks map[int]map[string]string) string {
	b, err := json.Marshal(tracks)
	if err != nil {
		panic(fmt.Sprintf("cannot json marshal snapd LTS tracks: %v", err))
	}
	return fmt.Sprintf("SNAPD_LTS_TRACKS='%s'", b)
}

func main() {
	fmt.Println(renderInfoLine(snapdLTSTracks))
}
