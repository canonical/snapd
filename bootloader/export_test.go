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

package bootloader

import (
	"io/ioutil"
	"os"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/bootloader/ubootenv"
)

// creates a new Androidboot bootloader object
func NewAndroidBoot() Bootloader {
	return newAndroidBoot()
}

func MockAndroidBootFile(c *C, mode os.FileMode) {
	f := &androidboot{}
	err := os.MkdirAll(f.dir(), 0755)
	c.Assert(err, IsNil)
	err = ioutil.WriteFile(f.ConfigFile(), nil, mode)
	c.Assert(err, IsNil)
}

func NewUboot() Bootloader {
	return newUboot()
}

func MockUbootFiles(c *C) {
	u := &uboot{}
	err := os.MkdirAll(u.dir(), 0755)
	c.Assert(err, IsNil)

	// ensure that we have a valid uboot.env too
	env, err := ubootenv.Create(u.envFile(), 4096)
	c.Assert(err, IsNil)
	err = env.Save()
	c.Assert(err, IsNil)
}

func NewGrub() Bootloader {
	return newGrub()
}

func MockGrubFiles(c *C) {
	g := &grub{}
	err := os.MkdirAll(g.dir(), 0755)
	c.Assert(err, IsNil)
	err = ioutil.WriteFile(g.ConfigFile(), nil, 0644)
	c.Assert(err, IsNil)
}
