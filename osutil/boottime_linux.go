// -*- Mode: Go; indent-tabs-mode: t -*-

//go:build linux

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

package osutil

import (
	"fmt"
	"os"
	"time"
)

// BootTime returns the approximate system boot time by reading
// /proc/uptime. Returns a zero time.Time if the boot time cannot be
// determined.
func BootTime() time.Time {
	data, err := os.ReadFile("/proc/uptime")
	if err != nil {
		return time.Time{}
	}
	var uptime float64
	if _, err := fmt.Sscanf(string(data), "%f", &uptime); err != nil {
		return time.Time{}
	}
	return time.Now().Add(-time.Duration(uptime * float64(time.Second)))
}
