// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2023 Canonical Ltd
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

package restart

import (
	"github.com/snapcore/snapd/boot"
	"github.com/snapcore/snapd/bootloader"
)

type RestartParameters struct {
	SnapName          string              `json:"snap-name,omitempty"`
	RestartType       RestartType         `json:"restart-type,omitempty"`
	BootloaderOptions *bootloader.Options `json:"bootloader-options,omitempty"`
}

// These are the restart-types that are relevant for the restart parameters. They
// are ordered by priority.
// Restart types of type *Now takes precedence.
var restartTypeOrder = []RestartType{
	// PowerOff takes precedence above Halt as it we perceive it as being the
	// stronger of those two requests.
	RestartSystemPoweroffNow,
	RestartSystemHaltNow,
	RestartSystemNow,
	RestartSystem,
}

func (rt *RestartParameters) init(snapName string, restartType RestartType, rebootInfo *boot.RebootInfo) {
	for _, r := range restartTypeOrder {
		// Only set if the one stored isn't already same priority
		// or higher.
		if rt.RestartType == r {
			break
		}
		if restartType == r {
			rt.SnapName = snapName
			rt.RestartType = restartType
			break
		}
	}

	// only override if the one we have stored is nil
	if rebootInfo != nil && rebootInfo.BootloaderOptions != nil && rt.BootloaderOptions == nil {
		rt.BootloaderOptions = rebootInfo.BootloaderOptions
	}
}
