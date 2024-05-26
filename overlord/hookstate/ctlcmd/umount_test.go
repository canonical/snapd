// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2022 Canonical Ltd
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
	"errors"

	. "gopkg.in/check.v1"

	"github.com/ddkwork/golibrary/mylog"
	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/overlord/hookstate"
	"github.com/snapcore/snapd/overlord/hookstate/ctlcmd"
	"github.com/snapcore/snapd/overlord/hookstate/hooktest"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/systemd"
	"github.com/snapcore/snapd/testutil"
)

type ParamsForListMountUnits struct {
	snapName string
	origin   string
}

type ResultForListMountUnits struct {
	units []string
	err   error
}

type FakeSystemdForUmount struct {
	systemd.Systemd

	RemoveMountUnitFileCalls       []string
	RemoveMountUnitFileCallsResult error

	ListMountUnitsCalls       []*ParamsForListMountUnits
	ListMountUnitsCallsResult ResultForListMountUnits
}

func (s *FakeSystemdForUmount) RemoveMountUnitFile(mountedDir string) error {
	s.RemoveMountUnitFileCalls = append(s.RemoveMountUnitFileCalls, mountedDir)
	return s.RemoveMountUnitFileCallsResult
}

func (s *FakeSystemdForUmount) ListMountUnits(snapName, origin string) ([]string, error) {
	s.ListMountUnitsCalls = append(s.ListMountUnitsCalls,
		&ParamsForListMountUnits{snapName, origin})
	return s.ListMountUnitsCallsResult.units, s.ListMountUnitsCallsResult.err
}

type umountSuite struct {
	testutil.BaseTest
	state       *state.State
	mockContext *hookstate.Context
	mockHandler *hooktest.MockHandler
	hookTask    *state.Task
	sysd        *FakeSystemdForUmount
}

var _ = Suite(&umountSuite{})

func (s *umountSuite) SetUpTest(c *C) {
	s.BaseTest.SetUpTest(c)
	dirs.SetRootDir(c.MkDir())

	s.mockHandler = hooktest.NewMockHandler()

	s.state = state.New(nil)
	s.state.Lock()
	defer s.state.Unlock()
	task := s.state.NewTask("test-task", "my test task")
	setup := &hookstate.HookSetup{Snap: "snap1", Revision: snap.R(42), Hook: "umount"}

	ctx := mylog.Check2(hookstate.NewContext(task, s.state, setup, s.mockHandler, ""))

	s.mockContext = ctx

	s.hookTask = task

	s.sysd = &FakeSystemdForUmount{}
	s.AddCleanup(systemd.MockNewSystemd(func(be systemd.Backend, roodDir string, mode systemd.InstanceMode, meter systemd.Reporter) systemd.Systemd {
		return s.sysd
	}))
}

func (s *umountSuite) TestMissingContext(c *C) {
	_, _ := mylog.Check3(ctlcmd.Run(nil, []string{"umount", "/dest"}, 0))
	c.Check(err, ErrorMatches, `cannot invoke snapctl operation commands \(here "umount"\) from outside of a snap`)
}

func (s *umountSuite) TestMissingParameters(c *C) {
	_, _ := mylog.Check3(ctlcmd.Run(s.mockContext, []string{"umount"}, 0))
	c.Check(err, ErrorMatches, "the required argument `<where>` was not provided")
}

func (s *umountSuite) TestListUnitFailure(c *C) {
	s.sysd.ListMountUnitsCallsResult = ResultForListMountUnits{[]string{}, errors.New("list error")}

	_, _ := mylog.Check3(ctlcmd.Run(s.mockContext, []string{"umount", "/dest"}, 0))
	c.Check(err, ErrorMatches, `cannot retrieve list of mount units: list error`)
	c.Check(s.sysd.ListMountUnitsCalls, DeepEquals, []*ParamsForListMountUnits{
		{"snap1", "mount-control"},
	})
	c.Check(s.sysd.RemoveMountUnitFileCalls, HasLen, 0)
}

func (s *umountSuite) TestUnitNotFound(c *C) {
	s.sysd.ListMountUnitsCallsResult = ResultForListMountUnits{[]string{
		"/this/is",
		"/not/our/mount/destination",
	}, nil}

	_, _ := mylog.Check3(ctlcmd.Run(s.mockContext, []string{"umount", "/dest"}, 0))
	c.Check(err, ErrorMatches, `cannot find the given mount`)
	c.Check(s.sysd.ListMountUnitsCalls, DeepEquals, []*ParamsForListMountUnits{
		{"snap1", "mount-control"},
	})
	c.Check(s.sysd.RemoveMountUnitFileCalls, HasLen, 0)
}

func (s *umountSuite) TestRemovalError(c *C) {
	s.sysd.ListMountUnitsCallsResult = ResultForListMountUnits{[]string{"/dest"}, nil}

	s.sysd.RemoveMountUnitFileCallsResult = errors.New("remove error")

	_, _ := mylog.Check3(ctlcmd.Run(s.mockContext, []string{"umount", "/dest"}, 0))
	c.Check(err, ErrorMatches, `cannot remove mount unit: remove error`)
	c.Check(s.sysd.ListMountUnitsCalls, DeepEquals, []*ParamsForListMountUnits{
		{"snap1", "mount-control"},
	})
	c.Check(s.sysd.RemoveMountUnitFileCalls, DeepEquals, []string{
		"/dest",
	})
}

func (s *umountSuite) TestHappy(c *C) {
	s.sysd.ListMountUnitsCallsResult = ResultForListMountUnits{[]string{"/dest"}, nil}

	_, _ := mylog.Check3(ctlcmd.Run(s.mockContext, []string{"umount", "/dest"}, 0))
	c.Check(err, IsNil)
	c.Check(s.sysd.ListMountUnitsCalls, DeepEquals, []*ParamsForListMountUnits{
		{"snap1", "mount-control"},
	})
	c.Check(s.sysd.RemoveMountUnitFileCalls, DeepEquals, []string{
		"/dest",
	})
}
