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

package lkenv_test

import (
	"io/ioutil"
	"path/filepath"
	"testing"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/bootloader/lkenv"
)

// Hook up check.v1 into the "go test" runner
func Test(t *testing.T) { TestingT(t) }

type lkenvTestSuite struct {
	envPath    string
	envPathbak string
}

var _ = Suite(&lkenvTestSuite{})

func (l *lkenvTestSuite) SetUpTest(c *C) {
	l.envPath = filepath.Join(c.MkDir(), "snapbootsel.bin")
	l.envPathbak = l.envPath + "bak"
}

func (l *lkenvTestSuite) TestSet(c *C) {
	env := lkenv.NewEnv(l.envPath)
	c.Check(env, NotNil)

	env.Set("snap_mode", "try")
	c.Check(env.Get("snap_mode"), Equals, "try")
}

func (l *lkenvTestSuite) TestSave(c *C) {
	buf := make([]byte, 4096)
	err := ioutil.WriteFile(l.envPathbak, buf, 0644)
	c.Assert(err, IsNil)
	l.TestSaveNoBak(c)
}

func (l *lkenvTestSuite) TestSaveNoBak(c *C) {
	buf := make([]byte, 4096)
	err := ioutil.WriteFile(l.envPath, buf, 0644)
	c.Assert(err, IsNil)

	env := lkenv.NewEnv(l.envPath)
	c.Check(env, NotNil)

	env.Set("snap_mode", "trying")
	env.Set("snap_kernel", "kernel-1")
	env.Set("snap_try_kernel", "kernel-2")
	env.Set("snap_core", "core-1")
	env.Set("snap_try_core", "core-2")
	env.Set("snap_gadget", "gadget-1")
	env.Set("snap_try_gadget", "gadget-2")

	err = env.Save()
	c.Assert(err, IsNil)

	env2 := lkenv.NewEnv(l.envPath)
	err = env2.Load()
	c.Assert(err, IsNil)
	c.Check(env2.Get("snap_mode"), Equals, "trying")
	c.Check(env2.Get("snap_kernel"), Equals, "kernel-1")
	c.Check(env2.Get("snap_try_kernel"), Equals, "kernel-2")
	c.Check(env2.Get("snap_core"), Equals, "core-1")
	c.Check(env2.Get("snap_try_core"), Equals, "core-2")
	c.Check(env2.Get("snap_gadget"), Equals, "gadget-1")
	c.Check(env2.Get("snap_try_gadget"), Equals, "gadget-2")
}

func (l *lkenvTestSuite) TestFailedCRC(c *C) {
	buf := make([]byte, 4096)
	err := ioutil.WriteFile(l.envPathbak, buf, 0644)
	c.Assert(err, IsNil)
	l.TestFailedCRCNoBak(c)
}

func (l *lkenvTestSuite) TestFailedCRCNoBak(c *C) {
	buf := make([]byte, 4096)
	err := ioutil.WriteFile(l.envPath, buf, 0644)
	c.Assert(err, IsNil)

	env := lkenv.NewEnv(l.envPath)
	c.Check(env, NotNil)

	err = env.Load()
	c.Assert(err, NotNil)
}

func (l *lkenvTestSuite) TestFailedCRCFallBack(c *C) {
	buf := make([]byte, 4096)
	err := ioutil.WriteFile(l.envPath, buf, 0644)
	c.Assert(err, IsNil)
	err = ioutil.WriteFile(l.envPathbak, buf, 0644)
	c.Assert(err, IsNil)

	env := lkenv.NewEnv(l.envPath)
	c.Check(env, NotNil)

	env.Set("snap_mode", "trying")
	env.Set("snap_kernel", "kernel-1")
	env.Set("snap_try_kernel", "kernel-2")
	err = env.Save()
	c.Assert(err, IsNil)

	// break main  env file
	err = ioutil.WriteFile(l.envPath, buf, 0644)
	c.Assert(err, IsNil)

	env2 := lkenv.NewEnv(l.envPath)
	err = env2.Load()
	c.Assert(err, IsNil)
	c.Check(env2.Get("snap_mode"), Equals, "trying")
	c.Check(env2.Get("snap_kernel"), Equals, "kernel-1")
	c.Check(env2.Get("snap_try_kernel"), Equals, "kernel-2")
}

func (l *lkenvTestSuite) TestFindFree_Set_Free_BootPartition(c *C) {
	buf := make([]byte, 4096)
	err := ioutil.WriteFile(l.envPath, buf, 0644)
	c.Assert(err, IsNil)

	env := lkenv.NewEnv(l.envPath)
	c.Assert(err, IsNil)
	env.ConfigureBootPartitions("boot_a", "boot_b")
	// test no boot partition used
	p, err := env.FindFreeBootPartition("kernel-1")
	c.Check(p, Equals, "boot_a")
	c.Assert(err, IsNil)
	//  set kernel-2 to boot_a partition
	err = env.SetBootPartition("boot_a", "kernel-2")
	c.Assert(err, IsNil)

	env.Set("snap_kernel", "kernel-2")
	// kernel-2 should now return first part, as it's already there
	p, err = env.FindFreeBootPartition("kernel-2")
	c.Check(p, Equals, "boot_a")
	c.Assert(err, IsNil)
	// test kernel-1 snapd, it should now offer second partition
	p, err = env.FindFreeBootPartition("kernel-1")
	c.Check(p, Equals, "boot_b")
	c.Assert(err, IsNil)
	err = env.SetBootPartition("boot_b", "kernel-1")
	c.Assert(err, IsNil)
	// set boot kernel-1
	env.Set("snap_kernel", "kernel-1")
	// now kernel-2 should not be protected and boot_a shoild be offered
	p, err = env.FindFreeBootPartition("kernel-3")
	c.Check(p, Equals, "boot_a")
	c.Assert(err, IsNil)
	err = env.SetBootPartition("boot_a", "kernel-3")
	c.Assert(err, IsNil)
	// remove kernel
	used, err := env.FreeBootPartition("kernel-3")
	c.Assert(err, IsNil)
	c.Check(used, Equals, true)
	// repeated use should return false and error
	used, err = env.FreeBootPartition("kernel-3")
	c.Assert(err, NotNil)
	c.Check(used, Equals, false)
}
