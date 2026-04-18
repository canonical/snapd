// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2019 Canonical Ltd
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

package policy

import (
	"errors"
	"strconv"
	"strings"
)

var (
	errNoName            = errors.New("snap has no name (not installed?)")
	errInUseForBoot      = errors.New("snap is being used for boot")
	errRequired          = errors.New("snap is required")
	errIsModel           = errors.New("snap is used by the model")
	errSnapdNotInstalled = errors.New("core snap cannot be removed if snapd snap is not installed")

	errSnapdNotRemovableOnCore       = errors.New("snapd required on core devices")
	errSnapdNotYetRemovableOnClassic = errors.New("remove all other snaps first")

	errEphemeralSnapsNotRemovable = errors.New("no snaps are removable in any of the ephemeral modes")
)

type inUseByErr []string

func (e inUseByErr) Error() string {
	switch len(e) {
	case 0:
		// how
		return "snap is being used"
	case 1:
		return "snap is being used by snap " + e[0] + "."
	case 2, 3, 4, 5:
		return "snap is being used by snaps " + strings.Join(e[:len(e)-1], ", ") + " and " + e[len(e)-1] + "."
	default:
		return "snap is being used by snaps " + strings.Join(e[:5], ", ") + " and " + strconv.Itoa(len(e)-5) + " more."
	}
}
