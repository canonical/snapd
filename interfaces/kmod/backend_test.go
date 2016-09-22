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

package kmod_test

import (
	"path/filepath"
	"testing"

	"github.com/snapcore/snapd/testutil"
	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/interfaces"
	"github.com/snapcore/snapd/interfaces/backendtest"
	"github.com/snapcore/snapd/interfaces/kmod"
	"github.com/snapcore/snapd/osutil"
)

func Test(t *testing.T) {
	TestingT(t)
}

type backendSuite struct {
	backendtest.BackendSuite
	modprobeCmd *testutil.MockCmd
}

var _ = Suite(&backendSuite{})

func (s *backendSuite) SetUpTest(c *C) {
	s.Backend = &kmod.Backend{}
	s.BackendSuite.SetUpTest(c)
	s.modprobeCmd = testutil.MockCommand(c, "modprobe", "")
}

func (s *backendSuite) TearDownTest(c *C) {
	s.modprobeCmd.Restore()
	s.BackendSuite.TearDownTest(c)
}

func (s *backendSuite) TestName(c *C) {
	c.Check(s.Backend.Name(), Equals, "kmod")
}

func (s *backendSuite) TestInstallingSnapCreatedModulesConf(c *C) {
	// NOTE: Hand out a permanent snippet so that .conf file is generated.
	s.Iface.PermanentSlotSnippetCallback = func(slot *interfaces.Slot, securitySystem interfaces.SecuritySystem) ([]byte, error) {
		if securitySystem == interfaces.SecurityKMod {
			return []byte("module1    \n    module2\nmodule1\n#\n"), nil
		}
		return nil, nil
	}

	path := filepath.Join(dirs.SnapKModModulesDir, "snap.samba.conf")
	c.Assert(osutil.FileExists(path), Equals, false)

	for _, devMode := range []bool{true, false} {
		s.modprobeCmd.ForgetCalls()
		snapInfo := s.InstallSnap(c, devMode, backendtest.SambaYamlV1, 0)

		c.Assert(osutil.FileExists(path), Equals, true)
		c.Assert(s.modprobeCmd.Calls(), DeepEquals, [][]string{
			{"modprobe", "--syslog", "module1"},
			{"modprobe", "--syslog", "module2"},
		})
		s.RemoveSnap(c, snapInfo)
	}
}

func (s *backendSuite) TestRemovingSnapRemovesModulesConf(c *C) {
	// NOTE: Hand out a permanent snippet so that .conf file is generated.
	s.Iface.PermanentSlotSnippetCallback = func(slot *interfaces.Slot, securitySystem interfaces.SecuritySystem) ([]byte, error) {
		if securitySystem == interfaces.SecurityKMod {
			return []byte("module1\nmodule2"), nil
		}
		return nil, nil
	}

	path := filepath.Join(dirs.SnapKModModulesDir, "snap.samba.conf")
	c.Assert(osutil.FileExists(path), Equals, false)

	for _, devMode := range []bool{true, false} {
		snapInfo := s.InstallSnap(c, devMode, backendtest.SambaYamlV1, 0)
		c.Assert(osutil.FileExists(path), Equals, true)
		s.RemoveSnap(c, snapInfo)
		c.Assert(osutil.FileExists(path), Equals, false)
	}
}

func (s *backendSuite) TestSecurityIsStable(c *C) {
	// NOTE: Hand out a permanent snippet so that .conf file is generated.
	s.Iface.PermanentSlotSnippetCallback = func(slot *interfaces.Slot, securitySystem interfaces.SecuritySystem) ([]byte, error) {
		if securitySystem == interfaces.SecurityKMod {
			return []byte("module1\nmodule2"), nil
		}
		return nil, nil
	}
	for _, devMode := range []bool{true, false} {
		snapInfo := s.InstallSnap(c, devMode, backendtest.SambaYamlV1, 0)
		s.modprobeCmd.ForgetCalls()
		err := s.Backend.Setup(snapInfo, devMode, s.Repo)
		c.Assert(err, IsNil)
		// modules conf is not re-loaded when nothing changes
		c.Check(s.modprobeCmd.Calls(), HasLen, 0)
		s.RemoveSnap(c, snapInfo)
	}
}
