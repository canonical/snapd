// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2021 Canonical Ltd
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
	"fmt"

	. "gopkg.in/check.v1"

	"github.com/ddkwork/golibrary/mylog"
	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/overlord/devicestate"
	"github.com/snapcore/snapd/overlord/hookstate"
	"github.com/snapcore/snapd/overlord/hookstate/ctlcmd"
	"github.com/snapcore/snapd/overlord/hookstate/hooktest"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/testutil"
)

type systemModeSuite struct {
	testutil.BaseTest
	st          *state.State
	mockHandler *hooktest.MockHandler
}

var _ = Suite(&systemModeSuite{})

func (s *systemModeSuite) SetUpTest(c *C) {
	s.BaseTest.SetUpTest(c)
	dirs.SetRootDir(c.MkDir())
	s.AddCleanup(func() { dirs.SetRootDir("/") })
	s.st = state.New(nil)
	s.mockHandler = hooktest.NewMockHandler()
}

func (s *systemModeSuite) TestSystemMode(c *C) {
	s.st.Lock()
	task := s.st.NewTask("test-task", "my test task")
	setup := &hookstate.HookSetup{Snap: "snap1", Revision: snap.R(1), Hook: "test-hook"}
	mockContext := mylog.Check2(hookstate.NewContext(task, s.st, setup, s.mockHandler, ""))
	c.Check(err, IsNil)
	s.st.Unlock()

	var smi *devicestate.SystemModeInfo
	var smiErr error
	r := ctlcmd.MockDevicestateSystemModeInfoFromState(func(s *state.State) (*devicestate.SystemModeInfo, error) {
		// the mocked function requires the state lock,
		// panic if it is not held
		s.Unlock()
		defer s.Lock()
		return smi, smiErr
	})
	defer r()

	tests := []struct {
		smi                 devicestate.SystemModeInfo
		smiErr              error
		stdout, stderr, err string
		exitCode            int
	}{
		{
			smiErr: fmt.Errorf("too early"),
			err:    "too early",
		}, {
			smi: devicestate.SystemModeInfo{
				Mode:   "run",
				Seeded: true,
			},
			stdout: "system-mode: run\nseed-loaded: true\n",
		}, {
			smi: devicestate.SystemModeInfo{
				Mode:       "install",
				HasModeenv: true,
				Seeded:     true,
				BootFlags:  []string{"factory"},
			},
			stdout: "system-mode: install\nseed-loaded: true\nfactory: true\n",
		}, {
			smi: devicestate.SystemModeInfo{
				Mode:       "run",
				HasModeenv: true,
				Seeded:     false,
			},
			stdout: "system-mode: run\nseed-loaded: false\n",
		},
	}

	for _, uid := range []uint32{0 /* root */, 1000 /* regular */} {
		for _, test := range tests {
			smi = &test.smi
			smiErr = test.smiErr

			stdout, stderr := mylog.Check3(ctlcmd.Run(mockContext, []string{"system-mode"}, uid))
			comment := Commentf("%v", test)
			if test.exitCode > 0 {
				c.Check(err, DeepEquals, &ctlcmd.UnsuccessfulError{ExitCode: test.exitCode}, comment)
			} else {
				if test.err == "" {
					c.Check(err, IsNil, comment)
				} else {
					c.Check(err, ErrorMatches, test.err, comment)
				}
			}

			c.Check(string(stdout), Equals, test.stdout, comment)
			c.Check(string(stderr), Equals, "", comment)
		}
	}
}
