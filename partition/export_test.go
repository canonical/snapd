// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2017 Canonical Ltd
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

package partition

import (
	"io/ioutil"
	"os"
	"path/filepath"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/dirs"
)

// creates a new Androidboot bootloader object
func MockNewAndroidboot() Bootloader {
	return newAndroidboot()
}

func MockAndroidbootFile(c *C, mode os.FileMode) {
	f := &androidboot{}
	newpath := filepath.Join(dirs.GlobalRootDir, "/boot/androidboot")
	os.MkdirAll(newpath, os.ModePerm)
	err := ioutil.WriteFile(f.ConfigFile(), nil, mode)
	c.Assert(err, IsNil)
}
