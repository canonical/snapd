// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2016 Canonical Ltd
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

package interfaces

import (
	"fmt"

	"github.com/ubuntu-core/snappy/snappy"
)

func activeSnapMetaDataImpl(snapName string) (version, origin string, apps []string, err error) {
	installed, err2 := snappy.NewLocalSnapRepository().Installed()
	if err2 != nil {
		err = fmt.Errorf("cannot list installed snaps: %v", err2)
		return
	}
	for _, snap := range installed {
		if snap.Name() == snapName && snap.IsActive() {
			// XXX: .Apps() should be in the interface
			for _, app := range snap.Apps() {
				apps = append(apps, app.Name)
			}
			version = snap.Version()
			origin = snap.Developer()
			return
		}
	}
	err = fmt.Errorf("there are no installed, active snaps with name %q", snapName)
	return
}

// ActiveSnapMetaData returns the version, origin and list of apps of a given snap.
var ActiveSnapMetaData = activeSnapMetaDataImpl
