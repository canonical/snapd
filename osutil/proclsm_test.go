// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2026 Canonical Ltd
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

package osutil_test

import (
	"os"
	"path/filepath"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/testutil"
)

type procLSMSuite struct {
	testutil.BaseTest
	fakeroot string
}

var _ = Suite(&procLSMSuite{})

func (s *procLSMSuite) SetUpTest(c *C) {
	s.BaseTest.SetUpTest(c)
	s.fakeroot = c.MkDir()
	dirs.SetRootDir(s.fakeroot)
	s.AddCleanup(func() { dirs.SetRootDir("") })
}

func (s *procLSMSuite) writeProc(c *C, rel, contents string) {
	path := filepath.Join(s.fakeroot, rel)
	c.Assert(os.MkdirAll(filepath.Dir(path), 0755), IsNil)
	c.Assert(os.WriteFile(path, []byte(contents), 0644), IsNil)
}

func (s *procLSMSuite) TestReadProcLSMCurrentSubdir(c *C) {
	s.writeProc(c, "proc/42/attr/apparmor/current", "snap.foo.app\n")
	s.writeProc(c, "proc/42/attr/current", "other-lsm-unread\n")

	label, err := osutil.ReadProcLSMCurrent(42, "apparmor")
	c.Assert(err, IsNil)
	c.Check(label, Equals, "snap.foo.app")
}

func (s *procLSMSuite) TestReadProcLSMCurrentLegacy(c *C) {
	s.writeProc(c, "proc/42/attr/current", "system_u:system_r:snappy_t:s0\n")

	label, err := osutil.ReadProcLSMCurrent(42, "selinux")
	c.Assert(err, IsNil)
	c.Check(label, Equals, "system_u:system_r:snappy_t:s0")
}

func (s *procLSMSuite) TestReadProcLSMCurrentMissing(c *C) {
	label, err := osutil.ReadProcLSMCurrent(42, "apparmor")
	c.Assert(err, IsNil)
	c.Check(label, Equals, "")
}
