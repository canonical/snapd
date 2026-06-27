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

package selinux_test

import (
	"os"
	"path/filepath"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/sandbox/selinux"
	"github.com/snapcore/snapd/testutil"
)

type selinuxProcessSuite struct {
	testutil.BaseTest
	fakeroot string
}

var _ = Suite(&selinuxProcessSuite{})

func (s *selinuxProcessSuite) SetUpTest(c *C) {
	s.BaseTest.SetUpTest(c)

	s.fakeroot = c.MkDir()
	dirs.SetRootDir(s.fakeroot)
	s.AddCleanup(func() { dirs.SetRootDir("") })

	restore := selinux.MockIsEnabled(func() (bool, error) { return true, nil })
	s.AddCleanup(restore)
	restore = selinux.MockIsEnforcing(func() (bool, error) { return true, nil })
	s.AddCleanup(restore)
}

func (s *selinuxProcessSuite) TestSecurityLabelFromPidLegacyPath(c *C) {
	procFile := filepath.Join(s.fakeroot, "proc/42/attr/current")
	c.Assert(os.MkdirAll(filepath.Dir(procFile), 0755), IsNil)
	c.Assert(os.WriteFile(procFile, []byte("system_u:system_r:snappy_t:s0\n"), 0644), IsNil)

	label, err := selinux.SecurityLabelFromPid(42)
	c.Assert(err, IsNil)
	c.Check(label, Equals, "system_u:system_r:snappy_t:s0")
}

func (s *selinuxProcessSuite) TestSecurityLabelFromPidSubdir(c *C) {
	procFile := filepath.Join(s.fakeroot, "proc/42/attr/selinux/current")
	c.Assert(os.MkdirAll(filepath.Dir(procFile), 0755), IsNil)
	c.Assert(os.WriteFile(procFile, []byte("unconfined_u:unconfined_r:unconfined_service_t:s0\n"), 0644), IsNil)

	label, err := selinux.SecurityLabelFromPid(42)
	c.Assert(err, IsNil)
	c.Check(label, Equals, "unconfined_u:unconfined_r:unconfined_service_t:s0")
}

func (s *selinuxProcessSuite) TestSecurityLabelFromPidUnsupported(c *C) {
	restore := selinux.MockIsEnabled(func() (bool, error) { return false, nil })
	defer restore()

	label, err := selinux.SecurityLabelFromPid(42)
	c.Assert(err, IsNil)
	c.Check(label, Equals, "")
}
