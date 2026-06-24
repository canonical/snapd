// -*- Mode: Go; indent-tabs-mode: t -*-
//go:build !nomanagers

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

package configcore

import "github.com/snapcore/snapd/strutil"

func init() {
	supportedConfigurations["core.system.disk-space-reservation"] = true
}

func validateDiskSpaceReservation(tr RunTransaction) error {
	reservation, err := coreCfg(tr, "system.disk-space-reservation")
	if err != nil {
		return err
	}
	if reservation == "" {
		return nil
	}
	_, err = strutil.ParseByteSize(reservation)
	return err
}
