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

package ctlcmd_test

import (
	"encoding/base64"
	"fmt"

	. "gopkg.in/check.v1"

	"github.com/ddkwork/golibrary/mylog"
	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/kernel/fde"
	"github.com/snapcore/snapd/overlord/hookstate"
	"github.com/snapcore/snapd/overlord/hookstate/ctlcmd"
	"github.com/snapcore/snapd/overlord/hookstate/hooktest"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/secboot/keys"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/testutil"
)

type fdeSetupSuite struct {
	testutil.BaseTest

	st          *state.State
	mockHandler *hooktest.MockHandler
	mockTask    *state.Task
	mockContext *hookstate.Context
}

var _ = Suite(&fdeSetupSuite{})

var mockFdeSetupKernelYaml = `name: pc-kernel
version: 1.0
type: kernel
hooks:
 fde-setup:
`

func (s *fdeSetupSuite) SetUpTest(c *C) {
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
		Snap:     "pc-kernel",
		Revision: snap.R(1),
		Hook:     "fde-setup",
	}
	context := mylog.Check2(hookstate.NewContext(s.mockTask, s.st, hooksup, s.mockHandler, ""))

	s.mockContext = context
}

func (s *fdeSetupSuite) TestFdeSetupRequestOpInvalid(c *C) {
	fdeSetup := &fde.SetupRequest{
		Op: "invalid-and-unknown",
	}
	s.mockContext.Lock()
	s.mockContext.Set("fde-setup-request", fdeSetup)
	s.mockContext.Unlock()

	stdout, stderr := mylog.Check3(ctlcmd.Run(s.mockContext, []string{"fde-setup-request"}, 0))
	c.Check(err, ErrorMatches, `unknown fde-setup-request op "invalid-and-unknown"`)
	c.Check(string(stdout), Equals, "")
	c.Check(string(stderr), Equals, "")
}

func (s *fdeSetupSuite) TestFdeSetupRequestNoFdeSetupOpData(c *C) {
	hooksup := &hookstate.HookSetup{
		Snap:     "pc-kernel",
		Revision: snap.R(1),
		Hook:     "other-hook",
	}
	context := mylog.Check2(hookstate.NewContext(nil, s.st, hooksup, s.mockHandler, ""))


	// check "fde-setup-request" error
	stdout, stderr := mylog.Check3(ctlcmd.Run(context, []string{"fde-setup-request"}, 0))
	c.Check(err, ErrorMatches, `cannot use fde-setup-request outside of the fde-setup hook`)
	c.Check(string(stdout), Equals, "")
	c.Check(string(stderr), Equals, "")

	// check "fde-setup-result" error
	stdout, stderr = mylog.Check3(ctlcmd.Run(context, []string{"fde-setup-result"}, 0))
	c.Check(err, ErrorMatches, `cannot use fde-setup-result outside of the fde-setup hook`)
	c.Check(string(stdout), Equals, "")
	c.Check(string(stderr), Equals, "")
}

func (s *fdeSetupSuite) TestFdeSetupRequestOpFeatures(c *C) {
	fdeSetup := &fde.SetupRequest{
		Op: "features",
	}
	s.mockContext.Lock()
	s.mockContext.Set("fde-setup-request", fdeSetup)
	s.mockContext.Unlock()

	stdout, stderr := mylog.Check3(ctlcmd.Run(s.mockContext, []string{"fde-setup-request"}, 0))

	c.Check(string(stdout), Equals, `{"op":"features"}`+"\n")
	c.Check(string(stderr), Equals, "")
}

func (s *fdeSetupSuite) TestFdeSetupRequestOpInitialSetup(c *C) {
	mockKey := keys.EncryptionKey{1, 2, 3, 4}
	fdeSetup := &fde.SetupRequest{
		Op:      "initial-setup",
		Key:     mockKey[:],
		KeyName: "the-key-name",
	}
	s.mockContext.Lock()
	s.mockContext.Set("fde-setup-request", fdeSetup)
	s.mockContext.Unlock()

	stdout, stderr := mylog.Check3(ctlcmd.Run(s.mockContext, []string{"fde-setup-request"}, 0))


	// the encryption key should be base64 encoded
	encodedBase64Key := base64.StdEncoding.EncodeToString(mockKey[:])

	c.Check(string(stdout), Equals, fmt.Sprintf(`{"op":"initial-setup","key":%q,"key-name":"the-key-name"}`+"\n", encodedBase64Key))
	c.Check(string(stderr), Equals, "")
}

func (s *fdeSetupSuite) TestFdeSetupResult(c *C) {
	mockStdin := []byte("sealed-key-data-from-stdin-as-set-by-daemon:runSnapctl")

	s.mockContext.Lock()
	s.mockContext.Set("stdin", mockStdin)
	s.mockContext.Unlock()

	stdout, stderr := mylog.Check3(ctlcmd.Run(s.mockContext, []string{"fde-setup-result"}, 0))

	c.Check(string(stdout), Equals, "")
	c.Check(string(stderr), Equals, "")

	// check that the task got the key that was passed via stdin
	var fdeSetupResult []byte
	s.mockContext.Lock()
	s.mockContext.Get("fde-setup-result", &fdeSetupResult)
	s.mockContext.Unlock()
	c.Check(fdeSetupResult, DeepEquals, mockStdin)
}
