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
	err := u.SetBootVar("snappy_mode", "regular")
	c.Assert(err, IsNil)
	err = u.SetBootVar("snappy_os", "4")
	c.Assert(err, IsNil)

	v, err := u.GetBootVar(bootmodeVar)
	c.Assert(err, IsNil)
	c.Assert(v, Equals, "regular")

	v, err = u.GetBootVar("snappy_os")
	c.Assert(err, IsNil)
	c.Assert(v, Equals, "4")
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
	env.Set("snappy_ab", "b")
	env.Set("snappy_mode", "regular")
	err = env.Save()
	c.Assert(err, IsNil)

	st, err := os.Stat(envFile)
	c.Assert(err, IsNil)
	time.Sleep(100 * time.Millisecond)

	u := newUboot()
	c.Assert(u, NotNil)

	// note that we set to the same var as above
	err = u.SetBootVar("snappy_ab", "b")
	c.Assert(err, IsNil)

	env, err = uenv.Open(envFile)
	c.Assert(err, IsNil)
	c.Assert(env.String(), Equals, "snappy_ab=b\nsnappy_mode=regular\n")

	st2, err := os.Stat(envFile)
	c.Assert(err, IsNil)
	c.Assert(st.ModTime(), Equals, st2.ModTime())
}

func (s *PartitionTestSuite) TestUbootSetBootVarFwEnv(c *C) {
	s.makeFakeUbootEnv(c)

	u := newUboot()
	err := u.SetBootVar("key", "value")
	c.Assert(err, IsNil)

	content, err := u.GetBootVar("key")
	c.Assert(err, IsNil)
	c.Assert(content, Equals, "value")
}

func (s *PartitionTestSuite) TestUbootGetBootVarFwEnv(c *C) {
	s.makeFakeUbootEnv(c)

	u := newUboot()
	err := u.SetBootVar("key2", "value2")
	c.Assert(err, IsNil)

	content, err := u.GetBootVar("key2")
	c.Assert(err, IsNil)
	c.Assert(content, Equals, "value2")
}
