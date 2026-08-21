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

package ltstrack

import "github.com/snapcore/snapd/snap"

// MockSnapdLTSTrackMap replaces this process's LTS track map for tests.
// The mocked snapd version is 2.75.
//
// Lives outside export_test.go because other packages' tests import it.
func MockSnapdLTSTrackMap(tracks map[int]map[string]string) (restore func()) {
	return snap.MockSnapdLTSTrackMapFromThis("2.75", tracks)
}
