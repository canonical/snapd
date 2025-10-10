// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2023-2024 Canonical Ltd
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

package integrity

import (
	"github.com/snapcore/snapd/snap/integrity/dmverity"
)

func MockVeritysetupFormat(fn func(string, string, *dmverity.DmVerityParams) (string, error)) (restore func()) {
	origVeritysetupFormat := veritysetupFormat
	veritysetupFormat = fn
	return func() {
		veritysetupFormat = origVeritysetupFormat
	}
}

func MockReadDmVeritySuperblock(f func(filename string) (*dmverity.VeritySuperblock, error)) (restore func()) {
	origReadDmVeritySuperblock := readDmVeritySuperblock
	readDmVeritySuperblock = f
	return func() {
		readDmVeritySuperblock = origReadDmVeritySuperblock
	}
}

func MockOsRename(fn func(string, string) error) (restore func()) {
	origOsRename := osRename
	osRename = fn
	return func() {
		osRename = origOsRename
	}
}
