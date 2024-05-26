// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2020 Canonical Ltd
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

package boot_test

import (
	. "gopkg.in/check.v1"

	"github.com/ddkwork/golibrary/mylog"
	"github.com/snapcore/snapd/boot"
	"github.com/snapcore/snapd/bootloader/bootloadertest"
	"github.com/snapcore/snapd/bootloader/efi"
	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/testutil"
)

var _ = Suite(&bootedKernelPartitionSuite{})

type bootedKernelPartitionSuite struct {
	testutil.BaseTest
}

func (s *bootedKernelPartitionSuite) SetUpTest(c *C) {
	dirs.SetRootDir(c.MkDir())
	s.AddCleanup(func() { dirs.SetRootDir("") })
}

func (s *bootedKernelPartitionSuite) TestFindPartitionUUIDForBootedKernelDisk(c *C) {
	restore := efi.MockVars(map[string][]byte{
		"LoaderDevicePartUUID-4a67b082-0a4c-41cf-b6c7-440b29bb8c4f": bootloadertest.UTF16Bytes("A9F5C949-AB89-5B47-A7BF-56DD28F96E65"),
	}, nil)
	defer restore()

	partuuid := mylog.Check2(boot.FindPartitionUUIDForBootedKernelDisk())

	c.Assert(partuuid, Equals, "a9f5c949-ab89-5b47-a7bf-56dd28f96e65")
}

func (s *bootedKernelPartitionSuite) TestFindPartitionUUIDForBootedKernelDiskNoEFISystem(c *C) {
	restore := efi.MockVars(nil, nil)
	defer restore()

	_ := mylog.Check2(boot.FindPartitionUUIDForBootedKernelDisk())
	c.Check(err, Equals, efi.ErrNoEFISystem)
}
