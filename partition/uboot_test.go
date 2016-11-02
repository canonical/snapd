// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2014-2015 Canonical Ltd
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
	"os"
	"time"

	"github.com/mvo5/uboot-go/uenv"

	. "gopkg.in/check.v1"
)

func (s *PartitionTestSuite) makeFakeUbootEnv(c *C) {
	u := &uboot{}

	// ensure that we have a valid uboot.env too
	env, err := uenv.Create(u.envFile(), 4096)
	c.Assert(err, IsNil)
	err = env.Save()
	c.Assert(err, IsNil)
}

func (s *PartitionTestSuite) TestNewUbootNoUbootReturnsNil(c *C) {
	u := newUboot()
	c.Assert(u, IsNil)
}

func (s *PartitionTestSuite) TestNewUboot(c *C) {
	s.makeFakeUbootEnv(c)

	u := newUboot()
	c.Assert(u, NotNil)
	c.Assert(u, FitsTypeOf, &uboot{})
}

func (s *PartitionTestSuite) TestUbootGetEnvVar(c *C) {
	s.makeFakeUbootEnv(c)

	u := newUboot()
	c.Assert(u, NotNil)
	err := u.SetBootVars(map[string]string{
		"snap_mode": "",
		"snap_core": "4",
	})
	c.Assert(err, IsNil)

	m, err := u.GetBootVars("snap_mode", "snap_core")
	c.Assert(err, IsNil)
	c.Assert(m, DeepEquals, map[string]string{
		"snap_mode": "",
		"snap_core": "4",
	})
}

func (s *PartitionTestSuite) TestGetBootloaderWithUboot(c *C) {
	s.makeFakeUbootEnv(c)

	bootloader, err := FindBootloader()
	c.Assert(err, IsNil)
	c.Assert(bootloader, FitsTypeOf, &uboot{})
}

func (s *PartitionTestSuite) TestUbootSetEnvNoUselessWrites(c *C) {
	s.makeFakeUbootEnv(c)

	envFile := (&uboot{}).envFile()
	env, err := uenv.Create(envFile, 4096)
	c.Assert(err, IsNil)
	env.Set("snap_ab", "b")
	env.Set("snap_mode", "")
	err = env.Save()
	c.Assert(err, IsNil)

	st, err := os.Stat(envFile)
	c.Assert(err, IsNil)
	time.Sleep(100 * time.Millisecond)

	u := newUboot()
	c.Assert(u, NotNil)

	// note that we set to the same var as above
	err = u.SetBootVars(map[string]string{"snap_ab": "b"})
	c.Assert(err, IsNil)

	env, err = uenv.Open(envFile)
	c.Assert(err, IsNil)
	c.Assert(env.String(), Equals, "snap_ab=b\n")

	st2, err := os.Stat(envFile)
	c.Assert(err, IsNil)
	c.Assert(st.ModTime(), Equals, st2.ModTime())
}

func (s *PartitionTestSuite) TestUbootSetBootVarFwEnv(c *C) {
	s.makeFakeUbootEnv(c)

	u := newUboot()
	err := u.SetBootVars(map[string]string{"key": "value"})
	c.Assert(err, IsNil)

	content, err := u.GetBootVars("key")
	c.Assert(err, IsNil)
	c.Assert(content, DeepEquals, map[string]string{"key": "value"})
}

func (s *PartitionTestSuite) TestUbootGetBootVarFwEnv(c *C) {
	s.makeFakeUbootEnv(c)

	u := newUboot()
	err := u.SetBootVars(map[string]string{"key2": "value2"})
	c.Assert(err, IsNil)

	content, err := u.GetBootVars("key2")
	c.Assert(err, IsNil)
	c.Assert(content, DeepEquals, map[string]string{"key2": "value2"})
}
