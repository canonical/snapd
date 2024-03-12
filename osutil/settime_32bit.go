// -*- Mode: Go; indent-tabs-mode: t -*-

//go:build (386 || arm) && linux

/*
 * Copyright (C) 2021 Canonical Ltd
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
	"syscall"
	"time"
)

func init() {
	timeToTimeval = timeToTimeval32
}

func timeToTimeval32(t time.Time) *syscall.Timeval {
	return &syscall.Timeval{
		Sec:  int32(t.Unix()),
		Usec: int32(t.Nanosecond() / 1000),
	}
}
