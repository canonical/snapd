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

package snap

// MockSnapdLTSTrackMapFromThis replaces SnapdLTSTrackMapFromThis for tests.
// version defaults to 2.75 when empty.
//
// Lives outside export_test.go because other packages' tests import it.
func MockSnapdLTSTrackMapFromThis(version string, tracks map[int]map[string]string) (restore func()) {
	restoreLoader := snapdLTSTrackMapFromThis
	if version == "" {
		version = "2.75"
	}
	snapdLTSTrackMapFromThis = func() (map[int]map[string]string, string, error) {
		return tracks, version, nil
	}
	return func() {
		snapdLTSTrackMapFromThis = restoreLoader
	}
}
