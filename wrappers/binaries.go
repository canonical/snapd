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
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/snap"
)

// AddSnapBinaries writes the wrapper binaries for the applications from the snap which aren't services.
func AddSnapBinaries(s *snap.Info) (err error) {
	var created []string
	defer func() {
		if err == nil {
			return
		}
		for _, fn := range created {
			os.Remove(fn)
		}
	}()

	if err := os.MkdirAll(dirs.SnapBinariesDir, 0755); err != nil {
		return err
	}

	noCompletion := !osutil.IsWritable(dirs.CompletersDir) || !osutil.FileExists(dirs.CompletersDir) || !osutil.FileExists(dirs.CompleteSh)
	for _, app := range s.Apps {
		if app.IsService() {
			continue
		}

		wrapperPath := app.WrapperPath()
		if err := os.Remove(wrapperPath); err != nil && !os.IsNotExist(err) {
			return err
		}
		if err := os.Symlink("/usr/bin/snap", wrapperPath); err != nil {
			return err
		}
		created = append(created, wrapperPath)

		if noCompletion || app.Completer == "" {
			continue
		}
		// symlink the completion snippet
		compPath := app.CompleterPath()
		if err := os.Symlink(dirs.CompleteSh, compPath); err == nil {
			created = append(created, compPath)
		} else if !os.IsExist(err) {
			return err
		}
	}

	return nil
}

// RemoveSnapBinaries removes the wrapper binaries for the applications from the snap which aren't services from.
func RemoveSnapBinaries(s *snap.Info) error {
	for _, app := range s.Apps {
		os.Remove(app.WrapperPath())
		if app.Completer == "" {
			continue
		}
		compPath := app.CompleterPath()
		if target, err := os.Readlink(compPath); err == nil && target == dirs.CompleteSh {
			os.Remove(compPath)
		}
	}

	return nil
}
