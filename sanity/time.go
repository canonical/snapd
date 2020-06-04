// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2020 Canonical Ltd
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

package sanity

import (
	"fmt"
	"time"
)

func init() {
	checks = append(checks, checkTime)
}

var timeNow = time.Now

func checkTime() error {
	now := timeNow()
	definitelyPast := time.Date(2020, 6, 4, 15, 44, 0, 0, time.UTC)
	if now.Before(definitelyPast) {
		// If the time is in the past of the constant above, then the device,
		// most likely, lacks battery powered RTC and did not use any other
		// means to synchronize the system clock. Since assertions and SSL
		// certificates rely on relatively accurate time, bail out early.
		return fmt.Errorf("current time %v is not realistic, clock is not set", now)
	}
	return nil
}
