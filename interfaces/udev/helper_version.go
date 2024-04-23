// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2024 Canonical Ltd
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

package udev

import (
	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/snapdtool"
	"github.com/snapcore/snapd/strutil"
)

var useOldCallCache *bool

func useOldCall() bool {
	if useOldCallCache != nil {
		return *useOldCallCache
	}
	version, _, err := snapdtool.SnapdVersionFromInfoFile(dirs.DistroLibExecDir)
	if err != nil {
		logger.Noticef("WARNING: could not find the version of the helper: %v", err)
		v := false
		useOldCallCache = &v
		return *useOldCallCache
	}
	cmp, err := strutil.VersionCompare(version, "2.62")
	if err != nil {
		logger.Noticef("WARNING: could parse the version of the helper: %v", err)
		v := false
		useOldCallCache = &v
		return *useOldCallCache
	}
	v := cmp < 0
	useOldCallCache = &v
	return *useOldCallCache
}
