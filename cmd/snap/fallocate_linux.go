// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2018 Canonical Ltd
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
	"os"
	"syscall"
)

func maybeReserveDiskSpace(f *os.File, size int64) error {
	const fallocKeepSize = 1 // This is FALLOC_FL_KEEP_SIZE
	if err := syscall.Fallocate(int(f.Fd()), fallocKeepSize, 0, size); err != nil {
		if err != syscall.EOPNOTSUPP && err != syscall.ENOSYS {
			return err
		}
	}
	return nil
}
