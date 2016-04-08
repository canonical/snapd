// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2016 Canonical Ltd
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

package snappy

import (
	"github.com/ubuntu-core/snappy/dirs"
	"github.com/ubuntu-core/snappy/partition"

	. "gopkg.in/check.v1"
)

type kernelTestSuite struct {
	bootloader *mockBootloader
}

var _ = Suite(&kernelTestSuite{})

func (s *kernelTestSuite) SetUpTest(c *C) {
	dirs.SetRootDir(c.MkDir())
	s.bootloader = newMockBootloader(c.MkDir())
	findBootloader = func() (partition.Bootloader, error) {
		return s.bootloader, nil
	}
}

func (s *kernelTestSuite) TestNameAndRevnoFromSnap(c *C) {
	name, revno := nameAndRevnoFromSnap("canonical-pc-linux.canonical_101.snap")
	c.Check(name, Equals, "canonical-pc-linux.canonical")
	c.Check(revno, Equals, 101)

	name, revno = nameAndRevnoFromSnap("ubuntu-core.canonical_103.snap")
	c.Check(name, Equals, "ubuntu-core.canonical")
	c.Check(revno, Equals, 103)
}

var kernelYaml = `name: linux
type: kernel
`

var osYaml = `name: core
type: os
`

func (s *kernelTestSuite) TestSyncBoot(c *C) {
	// make an OS
	_, err := makeInstalledMockSnap(osYaml+"version: v1", 10)
	c.Assert(err, IsNil)
	s.bootloader.SetBootVar("snappy_os", "core_10.snap")

	// make two kernels, v1 and v2 and activate v2
	_, err = makeInstalledMockSnap(kernelYaml+"version: v1", 20)
	c.Assert(err, IsNil)
	v2, err := makeInstalledMockSnap(kernelYaml+"version: v2", 21)
	c.Assert(err, IsNil)
	err = makeSnapActive(v2)
	c.Assert(err, IsNil)

	// ensure our mock env is correct, 3 snaps (1 os + 2 kernels)
	installed, err := (&Overlord{}).Installed()
	c.Assert(err, IsNil)
	c.Assert(installed, HasLen, 3)
	// ensure that v2 is the active one
	found := FindSnapsByNameAndRevision("linux", 21, installed)
	c.Assert(found, HasLen, 1)
	c.Assert(found[0].Name(), Equals, "linux")
	c.Assert(found[0].Revision(), Equals, 21)
	c.Assert(found[0].Version(), Equals, "v2")
	c.Assert(found[0].IsActive(), Equals, true)

	// Now we simulate that kernel v2 booted but failed
	// and the boot reverted to v1 in the bootloader environemnt.
	//
	// After such a failed boot the filesystem will still point
	// to v2 as the active version even though this is not true
	// because we booted with v1.
	s.bootloader.SetBootVar("snappy_kernel", "linux_20.snap")

	// run SyncBoot - this will correct the situation
	err = SyncBoot()
	c.Assert(err, IsNil)

	// ensure that v1 is active now
	installed, err = (&Overlord{}).Installed()
	c.Assert(err, IsNil)
	found = FindSnapsByNameAndVersion("linux", "v1", installed)
	c.Assert(found, HasLen, 1)
	c.Assert(found[0].Name(), Equals, "linux")
	c.Assert(found[0].Revision(), Equals, 20)
	c.Assert(found[0].Version(), Equals, "v1")
	c.Assert(found[0].IsActive(), Equals, true)
}
