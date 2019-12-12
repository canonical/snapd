// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2019 Canonical Ltd
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
	"io/ioutil"
	"os"
	"path/filepath"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/boot"
	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/testutil"
)

// baseBootSuite is used to setup the common test environment
type modeenvSuite struct {
	testutil.BaseTest

	tmpdir          string
	mockModeenvPath string
}

var _ = Suite(&modeenvSuite{})

func (s *modeenvSuite) SetUpTest(c *C) {
	s.tmpdir = c.MkDir()
	s.mockModeenvPath = filepath.Join(s.tmpdir, dirs.SnapModeenvFile)
}

func (s *modeenvSuite) TestUnset(c *C) {
	modeenv := &boot.Modeenv{}
	c.Check(modeenv.Unset(), Equals, true)
}

func (s *modeenvSuite) TestReadEmptyErrors(c *C) {
	modeenv, err := boot.ReadModeenv("/no/such/file")
	c.Assert(os.IsNotExist(err), Equals, true)
	c.Assert(modeenv, IsNil)
}

func (s *modeenvSuite) makeMockModeenvFile(c *C, content string) {
	err := os.MkdirAll(filepath.Dir(s.mockModeenvPath), 0755)
	c.Assert(err, IsNil)
	err = ioutil.WriteFile(s.mockModeenvPath, []byte(content), 0644)
	c.Assert(err, IsNil)
}

func (s *modeenvSuite) TestReadEmpty(c *C) {
	s.makeMockModeenvFile(c, "")

	modeenv, err := boot.ReadModeenv(s.tmpdir)
	c.Assert(err, IsNil)
	c.Check(modeenv.Mode, Equals, "")
	c.Check(modeenv.RecoverySystem, Equals, "")
	// an empty modeenv still means the modeenv was set
	c.Check(modeenv.Unset(), Equals, false)
}

func (s *modeenvSuite) TestReadMode(c *C) {
	s.makeMockModeenvFile(c, "mode=run")

	modeenv, err := boot.ReadModeenv(s.tmpdir)
	c.Assert(err, IsNil)
	c.Check(modeenv.Mode, Equals, "run")
	c.Check(modeenv.RecoverySystem, Equals, "")
	c.Check(modeenv.Base, Equals, "")
}

func (s *modeenvSuite) TestReadModeWithRecoverySystem(c *C) {
	s.makeMockModeenvFile(c, `mode=recovery
recovery_system=20191126
`)

	modeenv, err := boot.ReadModeenv(s.tmpdir)
	c.Assert(err, IsNil)
	c.Check(modeenv.Mode, Equals, "recovery")
	c.Check(modeenv.RecoverySystem, Equals, "20191126")
}

func (s *modeenvSuite) TestReadModeWithBase(c *C) {
	s.makeMockModeenvFile(c, `mode=recovery
recovery_system=20191126
base=core20_123.snap
kernel=pc-kernel_987.snap
`)

	modeenv, err := boot.ReadModeenv(s.tmpdir)
	c.Assert(err, IsNil)
	c.Check(modeenv.Mode, Equals, "recovery")
	c.Check(modeenv.RecoverySystem, Equals, "20191126")
	c.Check(modeenv.Base, Equals, "core20_123.snap")
	c.Check(modeenv.Kernel, Equals, "pc-kernel_987.snap")
}

func (s *modeenvSuite) TestWriteNonExisting(c *C) {
	c.Assert(s.mockModeenvPath, testutil.FileAbsent)

	modeenv := &boot.Modeenv{Mode: "run"}
	err := modeenv.Write(s.tmpdir)
	c.Assert(err, IsNil)

	c.Assert(s.mockModeenvPath, testutil.FileEquals, "mode=run\n")
}

func (s *modeenvSuite) TestWriteExisting(c *C) {
	s.makeMockModeenvFile(c, "mode=run")

	modeenv, err := boot.ReadModeenv(s.tmpdir)
	c.Assert(err, IsNil)
	modeenv.Mode = "recovery"
	err = modeenv.Write(s.tmpdir)
	c.Assert(err, IsNil)

	c.Assert(s.mockModeenvPath, testutil.FileEquals, "mode=recovery\n")
}

func (s *modeenvSuite) TestWriteNonExistingFull(c *C) {
	c.Assert(s.mockModeenvPath, testutil.FileAbsent)

	modeenv := &boot.Modeenv{
		Mode:           "run",
		RecoverySystem: "20191128",
		Base:           "core20_321.snap",
		Kernel:         "pc-kernel_456.snap",
	}
	err := modeenv.Write(s.tmpdir)
	c.Assert(err, IsNil)

	c.Assert(s.mockModeenvPath, testutil.FileEquals, `mode=run
recovery_system=20191128
base=core20_321.snap
kernel=pc-kernel_456.snap
`)
}
