// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2014-2016 Canonical Ltd
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

// Package wrappers is used to generate wrappers and service units and also desktop files for snap applications.
package wrappers

import (
	"os"

	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/snap"
)

// AddSnapBinaries writes the wrapper binaries for the applications from the snap which aren't services.
func AddSnapBinaries(s *snap.Info) error {
	if err := os.MkdirAll(dirs.SnapBinariesDir, 0755); err != nil {
		return err
	}

	for _, app := range s.Apps {
		if app.Daemon != "" {
			continue
		}

		if err := os.Remove(app.WrapperPath()); err != nil && !os.IsNotExist(err) {
			return err
		}
		if err := os.Symlink("/usr/bin/snap", app.WrapperPath()); err != nil {
			return err
		}
	}

	return nil
}

// RemoveSnapBinaries removes the wrapper binaries for the applications from the snap which aren't services from.
func RemoveSnapBinaries(s *snap.Info) error {
	for _, app := range s.Apps {
		os.Remove(app.WrapperPath())
	}

	return nil
}
