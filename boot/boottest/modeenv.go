// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2014-2019 Canonical Ltd
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

package boottest

import (
	"os"

	"github.com/snapcore/snapd/boot"
	"github.com/snapcore/snapd/dirs"
)

// ForceModeenv forces ReadModeenv to always return a specific Modeenv for a
// given root dir, returning a restore function to reset to the old behavior.
// If rootdir is empty, then all invocations return the specified modeenv
func ForceModeenv(rootdir string, m *boot.Modeenv) (restore func()) {
	mock := func(callerrootdir string) (*boot.Modeenv, error) {
		if rootdir == "" || rootdir == dirs.GlobalRootDir || callerrootdir == rootdir {
			return m, nil
		}

		// all other cases return doesn't exist
		return nil, os.ErrNotExist
	}

	return boot.MockReadModeenv(mock)
}
