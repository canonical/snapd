// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2020 Canonical Ltd
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

package naming

import (
	"github.com/snapcore/snapd/snapdenv"
)

var (
	prodWellKnownSnapIDs = map[string]string{
		"core":   "99T7MUlRhtI3U0QFgl5mXXESAiSwt776",
		"snapd":  "PMrrV4ml8uWuEUDBT8dSGnKUYbevVhc4",
		"core18": "CSO04Jhav2yK0uz97cr0ipQRyqg0qQL6",
		"core20": "DLqre5XGLbDqg9jPtiAhRRjDuPVa5X1q",
		"core22": "amcUKQILKXHHTlmSa7NMdnXSx02dNeeT",
	}

	stagingWellKnownSnapIDs = map[string]string{
		"core":   "xMNMpEm0COPZy7jq9YRwWVLCD9q5peow",
		"snapd":  "Z44rtQD1v4r1LXGPCDZAJO3AOw1EDGqy",
		"core18": "NhSvwckvNdvgdiVGlsO1vYmi3FPdTZ9U",
		// TODO:UC20 no core20 uploaded to staging yet
		"core20": "",
	}
)

var wellKnownSnapIDs = prodWellKnownSnapIDs

func init() {
	if snapdenv.UseStagingStore() {
		wellKnownSnapIDs = stagingWellKnownSnapIDs
	}
}

// WellKnownSnapID returns the snap-id of well-known snaps (snapd, core*)
// given the snap name or the empty string otherwise.
func WellKnownSnapID(snapName string) string {
	return wellKnownSnapIDs[snapName]
}

func UseStagingIDs(staging bool) (restore func()) {
	old := wellKnownSnapIDs
	if staging {
		wellKnownSnapIDs = stagingWellKnownSnapIDs
	} else {
		wellKnownSnapIDs = prodWellKnownSnapIDs
	}
	return func() {
		wellKnownSnapIDs = old
	}
}
