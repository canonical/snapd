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

	"github.com/snapcore/snapd/boot"
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
	restore := efi.MockEfiVariables(map[string][]byte{
		"LoaderDevicePartUUID-4a67b082-0a4c-41cf-b6c7-440b29bb8c4f": []byte("WHY-ARE-YOU-YELLING"),
	})
	defer restore()

	partuuid, err := boot.FindPartitionUUIDForBootedKernelDisk()
	c.Assert(err, IsNil)
	c.Assert(partuuid, Equals, "why-are-you-yelling")
}
