// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2017 Canonical Ltd
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

package main

import (
	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/osutil"
)

func debugShowProfile(profile *osutil.MountProfile, header string) {
	if len(profile.Entries) > 0 {
		logger.Debugf("%s:", header)
		for _, entry := range profile.Entries {
			logger.Debugf("\t%s", entry)
		}
	} else {
		logger.Debugf("%s: (none)", header)
	}
}

func debugShowChanges(changes []*Change, header string) {
	if len(changes) > 0 {
		logger.Debugf("%s:", header)
		for _, change := range changes {
			logger.Debugf("\t%s", change)
		}
	} else {
		logger.Debugf("%s: (none)", header)
	}
}
