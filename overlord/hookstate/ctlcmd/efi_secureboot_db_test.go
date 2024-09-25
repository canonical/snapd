// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2024 Canonical Ltd
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

package ctlcmd_test

import (
	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/overlord/hookstate"
	"github.com/snapcore/snapd/overlord/hookstate/ctlcmd"
	"github.com/snapcore/snapd/overlord/hookstate/hooktest"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/testutil"
)

type efiSecurebootDBUpdateSuite struct {
	testutil.BaseTest

	st          *state.State
	mockHandler *hooktest.MockHandler
	mockTask    *state.Task
	mockContext *hookstate.Context
}

var _ = Suite(&efiSecurebootDBUpdateSuite{})

func (s *efiSecurebootDBUpdateSuite) SetUpTest(c *C) {
	s.BaseTest.SetUpTest(c)
	dirs.SetRootDir(c.MkDir())
	s.AddCleanup(func() { dirs.SetRootDir("/") })

	s.st = state.New(nil)
	s.mockHandler = hooktest.NewMockHandler()
	s.st.Lock()
	defer s.st.Unlock()

	mockInstalledSnap(c, s.st, mockFdeSetupKernelYaml, "")
	s.mockTask = s.st.NewTask("test-task", "my test task")
	hooksup := &hookstate.HookSetup{
		Snap:     "efi-manager",
		Revision: snap.R(1),
	}
	context, err := hookstate.NewContext(s.mockTask, s.st, hooksup, s.mockHandler, "")
	c.Assert(err, IsNil)
	s.mockContext = context
}

func (s *efiSecurebootDBUpdateSuite) mockFwupdConnected(c *C) {
	s.st.Lock()
	defer s.st.Unlock()

	s.st.Set("conns", map[string]any{
		"efi-manager:fwupd efi-manager:fwupdmgr": map[string]any{
			"interface": "fwupd",
		},
	})
}

func (s *efiSecurebootDBUpdateSuite) TestInvalidArgs(c *C) {
	for _, tc := range []struct {
		args []string
		err  string
	}{{
		args: []string{"--startup", "--prepare"},
		err:  `--startup cannot be called with other actions`,
	}, {
		args: []string{"--startup", "--cleanup"},
		err:  `--startup cannot be called with other actions`,
	}, {
		args: []string{"--prepare", "--cleanup"},
		err:  `--prepare cannot be called with other actions`,
	}, {
		args: []string{"--prepare", "--dbx", "--pk", "--kek", "--db"},
		err:  `only one key database can be selected`,
	}, {
		args: []string{"--prepare", "--db"},
		err:  `updates of PK, KEK or DB are not supported`,
	}, {
		args: []string{"--prepare"},
		err:  `at least one database must be selected`,
	}, {
		args: []string{"--prepare", "--pk"},
		err:  `updates of PK, KEK or DB are not supported`,
	}, {
		args: []string{"--prepare", "--kek"},
		err:  `updates of PK, KEK or DB are not supported`,
	}, {
		args: []string{"--startup", "--dbx"},
		err:  `UEFI key database cannot be used with --startup`,
	}, {
		args: []string{"--cleanup", "--dbx"},
		err:  `UEFI key database cannot be used with --cleanup`,
	}} {
		c.Logf("calling %+v", tc)
		stdout, stderr, err := ctlcmd.Run(s.mockContext, append([]string{"efi-secureboot-db-update"}, tc.args...), 0)
		c.Check(err, ErrorMatches, tc.err)
		c.Check(string(stdout), Equals, "")
		c.Check(string(stderr), Equals, "")
	}
}

func (s *efiSecurebootDBUpdateSuite) TestOnlyAsRoot(c *C) {
	var uid uint32 = 123
	stdout, stderr, err := ctlcmd.Run(s.mockContext, []string{"efi-secureboot-db-update", "--startup"}, uid)
	c.Check(err, ErrorMatches, `cannot use "efi-secureboot-db-update" with uid 123, try with sudo`)
	c.Check(string(stdout), Equals, "")
	c.Check(string(stderr), Equals, "")
}

func (s *efiSecurebootDBUpdateSuite) TestPrepareNeedsPayload(c *C) {
	stdout, stderr, err := ctlcmd.Run(s.mockContext, []string{"efi-secureboot-db-update", "--prepare", "--dbx"}, 0)
	c.Check(err, ErrorMatches, `cannot extract payload: .*`)
	c.Check(string(stdout), Equals, "")
	c.Check(string(stderr), Equals, "")

	s.mockFwupdConnected(c)

	s.mockContext.Lock()
	s.mockContext.Set("stdin", []byte("payload"))
	s.mockContext.Unlock()
	stdout, stderr, err = ctlcmd.Run(s.mockContext, []string{"efi-secureboot-db-update", "--prepare", "--dbx"}, 0)
	c.Check(err, ErrorMatches, `not implemented`)
	c.Check(string(stdout), Equals, "")
	c.Check(string(stderr), Equals, "")
}

func (s *efiSecurebootDBUpdateSuite) TestChecksConnection(c *C) {
	s.mockContext.Lock()
	s.mockContext.Set("stdin", []byte("payload"))
	s.mockContext.Unlock()

	stdout, stderr, err := ctlcmd.Run(s.mockContext, []string{"efi-secureboot-db-update", "--prepare", "--dbx"}, 0)
	c.Check(err, ErrorMatches, `required interface fwupd is not connected`)
	c.Check(string(stdout), Equals, "")
	c.Check(string(stderr), Equals, "")

	s.mockFwupdConnected(c)

	stdout, stderr, err = ctlcmd.Run(s.mockContext, []string{"efi-secureboot-db-update", "--prepare", "--dbx"}, 0)
	c.Check(err, ErrorMatches, `not implemented`)
	c.Check(string(stdout), Equals, "")
	c.Check(string(stderr), Equals, "")
}
