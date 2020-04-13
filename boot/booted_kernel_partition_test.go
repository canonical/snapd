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
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/boot"
	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/osutil"
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
	// mock efivarfs
	dir := c.MkDir()

	efivarfsMount := `
38 24 0:32 / %s/efivars rw,nosuid,nodev,noexec,relatime shared:13 - efivarfs efivarfs rw
`
	restore := osutil.MockMountInfo(fmt.Sprintf(efivarfsMount[1:], dir))
	defer restore()

	// mock the efi var file
	varPath := filepath.Join(dir, "efivars", "LoaderDevicePartUUID-4a67b082-0a4c-41cf-b6c7-440b29bb8c4f")
	err := os.MkdirAll(filepath.Dir(varPath), 0755)
	c.Assert(err, IsNil)

	// we will also test that it is turned to all lower case
	err = ioutil.WriteFile(varPath, []byte("WHY-ARE-YOU-YELLING"), 0644)
	c.Assert(err, IsNil)

	partuuid, err := boot.FindPartitionUUIDForBootedKernelDisk()
	c.Assert(err, IsNil)
	c.Assert(partuuid, Equals, "why-are-you-yelling")
}
