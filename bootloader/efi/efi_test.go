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

package efi_test

import (
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"testing"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/bootloader/efi"
	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/testutil"
)

type efiVarsSuite struct {
	testutil.BaseTest
}

var _ = Suite(&efiVarsSuite{})

func TestBoot(t *testing.T) { TestingT(t) }

func (s *efiVarsSuite) SetUpTest(c *C) {
	s.BaseTest.SetUpTest(c)

	dirs.SetRootDir(c.MkDir())
	s.AddCleanup(func() { dirs.SetRootDir("") })
	restore := snap.MockSanitizePlugsSlots(func(snapInfo *snap.Info) {})
	s.AddCleanup(restore)
}

func (s *efiVarsSuite) TestReadEfiVarsUsesEfivarfs(c *C) {
	// make a dir separate from GlobalRootDir to use for efivarfs
	dir := c.MkDir()
	efivarfsMount := `
38 24 0:32 / %s/efivars rw,nosuid,nodev,noexec,relatime shared:13 - efivarfs efivarfs rw
`

	// mock the efi var file
	varPath := filepath.Join(dir, "efivars", "my-cool-efi-var")
	err := os.MkdirAll(filepath.Dir(varPath), 0755)
	c.Assert(err, IsNil)
	err = ioutil.WriteFile(varPath, []byte("blah-blah"), 0644)
	c.Assert(err, IsNil)

	restore := osutil.MockMountInfo(fmt.Sprintf(efivarfsMount[1:], dir))
	defer restore()

	data, err := efi.ReadEfiVar("my-cool-efi-var")
	c.Assert(err, IsNil)
	c.Assert(string(data), Equals, "blah-blah")
}

func (s *efiVarsSuite) TestReadEfiVarsFallsBackToSysfs(c *C) {
	// mock the efi var file under dirs.GlobalRootDir at the sysfs location
	varPath := filepath.Join(dirs.GlobalRootDir, "/sys/firmware/efi/vars", "my-cool-efi-var", "data")
	err := os.MkdirAll(filepath.Dir(varPath), 0755)
	c.Assert(err, IsNil)
	err = ioutil.WriteFile(varPath, []byte("blah-blah"), 0644)
	c.Assert(err, IsNil)

	// mock an empty mountinfo
	restore := osutil.MockMountInfo("")
	defer restore()

	data, err := efi.ReadEfiVar("my-cool-efi-var")
	c.Assert(err, IsNil)
	c.Assert(string(data), Equals, "blah-blah")
}

func (s *efiVarsSuite) TestReadEfiVarsNoProcfsTriesDefaultMountPoint(c *C) {
	// mock the efi var file under dirs.GlobalRootDir at the default mountpoint
	// for efivarfs
	varPath := filepath.Join(dirs.GlobalRootDir, "/sys/firmware/efivars", "my-cool-efi-var")
	err := os.MkdirAll(filepath.Dir(varPath), 0755)
	c.Assert(err, IsNil)
	err = ioutil.WriteFile(varPath, []byte("blah-blah"), 0644)
	c.Assert(err, IsNil)

	// mock a garbage mountinfo so it doesn't know if it's mounted or not
	restore := osutil.MockMountInfo("garbage")
	defer restore()

	data, err := efi.ReadEfiVar("my-cool-efi-var")
	c.Assert(err, IsNil)
	c.Assert(string(data), Equals, "blah-blah")
}

func (s *efiVarsSuite) TestReadEfiVarsNoProcfsNoEfivarfsFallsBackToSysfs(c *C) {
	// mock the efi var file under dirs.GlobalRootDir at the default mountpoint
	// for efivarfs
	varPath := filepath.Join(dirs.GlobalRootDir, "/sys/firmware/efi/vars", "my-cool-efi-var", "data")
	err := os.MkdirAll(filepath.Dir(varPath), 0755)
	c.Assert(err, IsNil)
	err = ioutil.WriteFile(varPath, []byte("blah-blah"), 0644)
	c.Assert(err, IsNil)

	// mock a garbage mountinfo so it doesn't know if it's mounted or not
	restore := osutil.MockMountInfo("garbage")
	defer restore()

	data, err := efi.ReadEfiVar("my-cool-efi-var")
	c.Assert(err, IsNil)
	c.Assert(string(data), Equals, "blah-blah")
}
