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

package boot_test

import (
	"os"
	"path/filepath"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/boot"
	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/testutil"
)

var _ = Suite(&sysrootStateSuite{})

type sysrootStateSuite struct {
	testutil.BaseTest
}

func (s *sysrootStateSuite) SetUpTest(c *C) {
	s.BaseTest.SetUpTest(c)
	dirs.SetRootDir(c.MkDir())
	s.AddCleanup(func() { dirs.SetRootDir("") })
}

func (s *sysrootStateSuite) writeSysrootState(c *C, content string) {
	c.Assert(os.MkdirAll(dirs.SnapBootstrapRunDir, 0755), IsNil)
	c.Assert(os.WriteFile(filepath.Join(dirs.SnapBootstrapRunDir, "sysroot.json"), []byte(content), 0644), IsNil)
}

func (s *sysrootStateSuite) TestLoadSysrootStateIntegrity(c *C) {
	s.writeSysrootState(c, `{"integrity":{"type":"dm-verity"}}`)

	st, err := boot.LoadSysrootState()
	c.Assert(err, IsNil)
	c.Check(st, DeepEquals, &boot.SysrootState{
		Integrity: &boot.SysrootIntegrity{Type: "dm-verity"},
	})
}

func (s *sysrootStateSuite) TestLoadSysrootStateNoIntegrity(c *C) {
	s.writeSysrootState(c, `{}`)

	st, err := boot.LoadSysrootState()
	c.Assert(err, IsNil)
	c.Check(st.Integrity, IsNil)
}

func (s *sysrootStateSuite) TestLoadSysrootStateMissing(c *C) {
	_, err := boot.LoadSysrootState()
	c.Check(err, testutil.ErrorIs, os.ErrNotExist)
}

func (s *sysrootStateSuite) TestLoadSysrootStateMalformed(c *C) {
	s.writeSysrootState(c, `{`)

	_, err := boot.LoadSysrootState()
	c.Check(err, ErrorMatches, "unexpected end of JSON input")
}
