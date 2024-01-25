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

package boot

import (
	"github.com/snapcore/snapd/dirs"
)

// ForceReseal will reseal the device even if no change requires resealing
func ForceReseal(unlocker Unlocker) error {
	modeenvLock()
	defer modeenvUnlock()

	modeenv, err := loadModeenv()
	if err != nil {
		return err
	}

	options := &ResealToModeenvOptions{
		ExpectReseal: true,
		Force:        true,
	}
	return resealKeyToModeenv(dirs.GlobalRootDir, modeenv, options, unlocker)
}
