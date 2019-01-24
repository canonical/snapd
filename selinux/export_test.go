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
package selinux

import (
	"io/ioutil"

	"gopkg.in/check.v1"
)

var (
	GetSELinuxMount = getSELinuxMount
)

func MockMountInfo(c *check.C, text string) (where string, restore func()) {
	old := procSelfMountInfo
	dir := c.MkDir()
	f, err := ioutil.TempFile(dir, "mountinfo")
	c.Assert(err, check.IsNil)
	err = ioutil.WriteFile(f.Name(), []byte(text), 0644)
	c.Assert(err, check.IsNil)
	procSelfMountInfo = f.Name()
	restore = func() {
		procSelfMountInfo = old
	}
	return procSelfMountInfo, restore
}
