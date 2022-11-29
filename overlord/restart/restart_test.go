// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2016-2022 Canonical Ltd
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

package restart_test

import (
	"bytes"
	"errors"
	"path/filepath"
	"testing"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/boot"
	"github.com/snapcore/snapd/bootloader/bootloadertest"
	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/overlord"
	"github.com/snapcore/snapd/overlord/restart"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/release"
	"github.com/snapcore/snapd/testutil"
)

func TestRestart(t *testing.T) { TestingT(t) }

type restartSuite struct{}

var _ = Suite(&restartSuite{})

type testHandler struct {
	restartRequested   bool
	rebootAsExpected   bool
	rebootDidNotHappen bool
	rebootInfo         *boot.RebootInfo
}

func (h *testHandler) HandleRestart(t restart.RestartType, rbi *boot.RebootInfo) {
	h.restartRequested = true
	h.rebootInfo = rbi
}

func (h *testHandler) RebootAsExpected(*state.State) error {
	h.rebootAsExpected = true
	return nil
}

func (h *testHandler) RebootDidNotHappen(*state.State) error {
	h.rebootDidNotHappen = true
	return nil
}

func (s *restartSuite) TestManager(c *C) {
	st := state.New(nil)

	st.Lock()
	defer st.Unlock()

	mgr, err := restart.Manager(st, "boot-id-1", nil)
	c.Assert(err, IsNil)
	c.Check(mgr, FitsTypeOf, &restart.RestartManager{})
}

func (s *restartSuite) TestRequestRestartDaemon(c *C) {
	st := state.New(nil)

	st.Lock()
	defer st.Unlock()

	// uninitialized
	ok, t := restart.Pending(st)
	c.Check(ok, Equals, false)
	c.Check(t, Equals, restart.RestartUnset)

	h := &testHandler{}

	_, err := restart.Manager(st, "boot-id-1", h)
	c.Assert(err, IsNil)
	c.Check(h.rebootAsExpected, Equals, true)

	ok, t = restart.Pending(st)
	c.Check(ok, Equals, false)
	c.Check(t, Equals, restart.RestartUnset)

	restart.Request(st, restart.RestartDaemon, nil)

	c.Check(h.restartRequested, Equals, true)

	ok, t = restart.Pending(st)
	c.Check(ok, Equals, true)
	c.Check(t, Equals, restart.RestartDaemon)
}

func (s *restartSuite) TestRequestRestartDaemonNoHandler(c *C) {
	st := state.New(nil)

	st.Lock()
	defer st.Unlock()

	_, err := restart.Manager(st, "boot-id-1", nil)
	c.Assert(err, IsNil)

	restart.Request(st, restart.RestartDaemon, nil)

	ok, t := restart.Pending(st)
	c.Check(ok, Equals, true)
	c.Check(t, Equals, restart.RestartDaemon)
}

func (s *restartSuite) TestRequestRestartSystemAndVerifyReboot(c *C) {
	st := state.New(nil)
	st.Lock()
	defer st.Unlock()

	h := &testHandler{}
	_, err := restart.Manager(st, "boot-id-1", h)
	c.Assert(err, IsNil)
	c.Check(h.rebootAsExpected, Equals, true)

	ok, t := restart.Pending(st)
	c.Check(ok, Equals, false)
	c.Check(t, Equals, restart.RestartUnset)

	restart.Request(st, restart.RestartSystem, nil)

	c.Check(h.restartRequested, Equals, true)

	ok, t = restart.Pending(st)
	c.Check(ok, Equals, true)
	c.Check(t, Equals, restart.RestartSystem)

	var fromBootID string
	c.Check(st.Get("system-restart-from-boot-id", &fromBootID), IsNil)
	c.Check(fromBootID, Equals, "boot-id-1")

	h1 := &testHandler{}
	_, err = restart.Manager(st, "boot-id-1", h1)
	c.Assert(err, IsNil)
	c.Check(h1.rebootAsExpected, Equals, false)
	c.Check(h1.rebootDidNotHappen, Equals, true)
	fromBootID = ""
	c.Check(st.Get("system-restart-from-boot-id", &fromBootID), IsNil)
	c.Check(fromBootID, Equals, "boot-id-1")

	h2 := &testHandler{}
	_, err = restart.Manager(st, "boot-id-2", h2)
	c.Assert(err, IsNil)
	c.Check(h2.rebootAsExpected, Equals, true)
	c.Check(st.Get("system-restart-from-boot-id", &fromBootID), testutil.ErrorIs, state.ErrNoState)
}

func (s *restartSuite) TestRequestRestartSystemWithRebootInfo(c *C) {
	st := state.New(nil)
	st.Lock()
	defer st.Unlock()

	h := &testHandler{}
	_, err := restart.Manager(st, "boot-id-1", h)
	c.Assert(err, IsNil)
	c.Check(h.rebootAsExpected, Equals, true)

	ok, t := restart.Pending(st)
	c.Check(ok, Equals, false)
	c.Check(t, Equals, restart.RestartUnset)

	restart.Request(st, restart.RestartSystem, &boot.RebootInfo{
		RebootRequired:   true,
		RebootBootloader: &bootloadertest.MockRebootBootloader{}})

	c.Check(h.restartRequested, Equals, true)
	c.Check(h.rebootInfo.RebootRequired, Equals, true)
	c.Check(h.rebootInfo.RebootBootloader, NotNil)

	ok, t = restart.Pending(st)
	c.Check(ok, Equals, true)
	c.Check(t, Equals, restart.RestartSystem)

	var fromBootID string
	c.Check(st.Get("system-restart-from-boot-id", &fromBootID), IsNil)
	c.Check(fromBootID, Equals, "boot-id-1")

	h1 := &testHandler{}
	_, err = restart.Manager(st, "boot-id-1", h1)
	c.Assert(err, IsNil)
	c.Check(h1.rebootAsExpected, Equals, false)
	c.Check(h1.rebootDidNotHappen, Equals, true)
	fromBootID = ""
	c.Check(st.Get("system-restart-from-boot-id", &fromBootID), IsNil)
	c.Check(fromBootID, Equals, "boot-id-1")

	h2 := &testHandler{}
	_, err = restart.Manager(st, "boot-id-2", h2)
	c.Assert(err, IsNil)
	c.Check(h2.rebootAsExpected, Equals, true)
	c.Check(st.Get("system-restart-from-boot-id", &fromBootID), testutil.ErrorIs, state.ErrNoState)
}

func (s *restartSuite) TestFinishTaskWithRestart(c *C) {
	st := state.New(nil)

	st.Lock()
	defer st.Unlock()

	defer release.MockOnClassic(false)()

	_, err := restart.Manager(st, "boot-id-1", nil)
	c.Assert(err, IsNil)

	tests := []struct {
		initial, final state.Status
		restartType    restart.RestartType
		classic        bool
		restart        bool
		wait           bool
		log            string
	}{
		{initial: state.DoStatus, final: state.DoneStatus, restartType: restart.RestartDaemon, classic: false, restart: true},
		{initial: state.DoStatus, final: state.DoneStatus, restartType: restart.RestartDaemon, classic: true, restart: true},
		{initial: state.UndoStatus, final: state.UndoneStatus, restartType: restart.RestartDaemon, classic: false, restart: true},
		{initial: state.DoStatus, final: state.DoneStatus, restartType: restart.RestartSystem, classic: false, restart: true, log: ".* INFO Requested system restart"},
		{initial: state.DoStatus, final: state.DoneStatus, restartType: restart.RestartSystem, classic: true, restart: false, wait: true, log: ".* INFO Task set to wait until a manual system restart allows to continue"},
		{initial: state.DoStatus, final: state.DoneStatus, restartType: restart.RestartSystemNow, classic: true, restart: false, wait: true, log: ".* INFO Task set to wait until a manual system restart allows to continue"},
		{initial: state.UndoStatus, final: state.UndoneStatus, restartType: restart.RestartSystem, classic: true, restart: false, log: ".* INFO Skipped automatic system restart on classic system when undoing changes back to previous state"},
		{initial: state.UndoStatus, final: state.UndoneStatus, restartType: restart.RestartSystem, classic: false, restart: true, log: ".* INFO Requested system restart"},
	}

	chg := st.NewChange("chg", "...")
	waitCount := 0

	for _, t := range tests {
		restart.MockPending(st, restart.RestartUnset)
		release.MockOnClassic(t.classic)

		task := st.NewTask("foo", "...")
		chg.AddTask(task)
		task.SetStatus(t.initial)

		err := restart.FinishTaskWithRestart(task, t.final, t.restartType, "some-snap", nil)
		setStatus := t.final
		if t.wait {
			setStatus = state.WaitStatus
		}
		c.Check(task.Status(), Equals, setStatus)
		var waitBootID string
		if err := task.Get("wait-for-system-restart-from-boot-id", &waitBootID); !errors.Is(err, state.ErrNoState) {
			c.Check(err, IsNil)
		}
		ok, rst := restart.Pending(st)
		if t.restart {
			c.Check(err, IsNil)
			c.Check(ok, Equals, true)
			c.Check(rst, Equals, t.restartType)
			c.Check(waitBootID, Equals, "")
		} else {
			if t.wait {
				waitCount++
				c.Check(err, DeepEquals, &state.Wait{Reason: "waiting for manual system restart"})
				c.Check(waitBootID, Equals, "boot-id-1")
				var wait bool
				c.Check(chg.Get("wait-for-system-restart", &wait), IsNil)
				c.Check(wait, Equals, waitCount != 0)
			} else {
				c.Check(err, IsNil)
				c.Check(waitBootID, Equals, "")
			}
			c.Check(ok, Equals, false)
		}
		if t.log == "" {
			c.Check(task.Log(), HasLen, 0)
		} else {
			c.Check(task.Log(), HasLen, 1)
			c.Check(task.Log()[0], Matches, t.log)
		}
	}
}

func (s *restartSuite) TestStartUpWaitTasks(c *C) {
	st := state.New(nil)

	st.Lock()
	defer st.Unlock()

	defer release.MockOnClassic(true)()

	rm, err := restart.Manager(st, "boot-id-1", nil)
	c.Assert(err, IsNil)

	chg := st.NewChange("chg", "...")
	t0 := st.NewTask("todo", "...")
	// needed in change otherwise the change is considered ready
	chg.AddTask(t0)

	t1 := st.NewTask("wait", "...")
	t1.SetStatus(state.WaitStatus)
	chg.AddTask(t1)

	t2 := st.NewTask("wait-for-reboot", "...")
	chg.AddTask(t2)
	err = restart.FinishTaskWithRestart(t2, state.DoneStatus, restart.RestartSystem, "some-snap", nil)
	c.Assert(err, FitsTypeOf, &state.Wait{})

	restart.ReplaceBootID(st, "boot-id-2")

	t3 := st.NewTask("wait-for-reboot-same-boot", "...")
	chg.AddTask(t3)
	err = restart.FinishTaskWithRestart(t3, state.DoneStatus, restart.RestartSystem, "some-snap", nil)
	c.Assert(err, FitsTypeOf, &state.Wait{})

	c.Assert(chg.IsReady(), Equals, false)

	se := overlord.NewStateEngine(st)
	se.AddManager(rm)
	st.Unlock()
	err = se.StartUp()
	st.Lock()
	c.Assert(err, IsNil)

	// no boot id is set in the task, status does not change
	c.Check(t1.Status(), Equals, state.WaitStatus)
	// same boot id in task/system, status does not change
	c.Check(t3.Status(), Equals, state.WaitStatus)
	// old boot id in task, task marked DoneStatus
	c.Check(t2.Status(), Equals, state.DoneStatus)

	var wait bool
	c.Check(chg.Get("wait-for-system-restart", &wait), IsNil)
	c.Check(wait, Equals, true)

	// another boot
	restart.ReplaceBootID(st, "boot-id-3")

	se = overlord.NewStateEngine(st)
	se.AddManager(rm)
	st.Unlock()
	err = se.StartUp()
	st.Lock()
	c.Assert(err, IsNil)

	c.Check(t1.Status(), Equals, state.WaitStatus)
	c.Check(t3.Status(), Equals, state.DoneStatus)

	c.Check(chg.Has("wait-for-system-restart"), Equals, false)
}

func (s *restartSuite) TestPendingForSystemRestart(c *C) {
	st := state.New(nil)

	st.Lock()
	defer st.Unlock()

	rm, err := restart.Manager(st, "boot-id-1", nil)
	c.Assert(err, IsNil)

	chg1 := st.NewChange("not-ready", "...")
	t1 := st.NewTask("task", "...")
	chg1.AddTask(t1)
	c.Check(chg1.IsReady(), Equals, false)

	chg2 := st.NewChange("not-pending", "...")
	t2 := st.NewTask("wait-task", "...")
	t3 := st.NewTask("task", "...")
	t4 := st.NewTask("task", "...")
	chg2.AddTask(t2)
	chg2.AddTask(t3)
	chg2.AddTask(t4)
	t3.WaitFor(t2)
	t4.WaitFor(t2)
	err = restart.FinishTaskWithRestart(t2, state.DoneStatus, restart.RestartSystem, "some-snap", nil)
	c.Assert(err, FitsTypeOf, &state.Wait{})
	t3.SetStatus(state.UndoStatus)
	t4.SetStatus(state.WaitStatus)
	c.Check(chg2.IsReady(), Equals, false)

	chg3 := st.NewChange("pending", "...")
	t5 := st.NewTask("wait-task", "...")
	t6 := st.NewTask("task", "...")
	t7 := st.NewTask("task", "...")
	chg3.AddTask(t5)
	chg3.AddTask(t6)
	chg3.AddTask(t7)
	t6.WaitFor(t5)
	t7.WaitFor(t5)
	c.Check(t5.Status(), Equals, state.DoStatus)
	err = restart.FinishTaskWithRestart(t5, state.DoneStatus, restart.RestartSystem, "some-snap", nil)
	c.Assert(err, FitsTypeOf, &state.Wait{})
	t6.SetStatus(state.WaitStatus)
	t7.SetStatus(state.DoStatus)
	c.Check(chg3.IsReady(), Equals, false)

	chg4 := st.NewChange("pending", "...")
	t8 := st.NewTask("wait-task", "...")
	chg4.AddTask(t8)
	c.Check(t8.Status(), Equals, state.DoStatus)
	// nothing after t8
	err = restart.FinishTaskWithRestart(t8, state.DoneStatus, restart.RestartSystem, "some-snap", nil)
	c.Assert(err, FitsTypeOf, &state.Wait{})
	c.Check(chg4.IsReady(), Equals, false)

	c.Check(rm.PendingForSystemRestart(chg1), Equals, false)
	c.Check(rm.PendingForSystemRestart(chg2), Equals, false)
	c.Check(rm.PendingForSystemRestart(chg3), Equals, true)
	c.Check(rm.PendingForSystemRestart(chg4), Equals, true)
}

type notifyRebootRequiredSuite struct {
	testutil.BaseTest

	st          *state.State
	mockNrrPath string
	mockLog     *bytes.Buffer
	t1          *state.Task
}

var _ = Suite(&notifyRebootRequiredSuite{})

func (s *notifyRebootRequiredSuite) SetUpTest(c *C) {
	s.BaseTest.SetUpTest(c)

	s.AddCleanup(release.MockOnClassic(true))

	s.st = state.New(nil)

	mockLog, restore := logger.MockLogger()
	s.AddCleanup(restore)
	s.mockLog = mockLog

	dirs.SetRootDir(c.MkDir())
	s.mockNrrPath = filepath.Join(dirs.GlobalRootDir, "/usr/share/update-notifier/notify-reboot-required")

	s.st.Lock()
	defer s.st.Unlock()

	_, err := restart.Manager(s.st, "boot-id-1", nil)
	c.Assert(err, IsNil)

	// pretend there is a snap that requires a reboot
	chg1 := s.st.NewChange("not-ready", "...")
	s.t1 = s.st.NewTask("task", "...")
	chg1.AddTask(s.t1)
}

func (s *notifyRebootRequiredSuite) TestFinishTaskWithRestartNotifiesRebootRequired(c *C) {
	s.st.Lock()
	defer s.st.Unlock()

	mockNrr := testutil.MockCommand(c, s.mockNrrPath, `
test "$DPKG_MAINTSCRIPT_PACAGE" = "snap:some-snap"
test "$DPKG_MAINTSCRIPT_NAME" = "postinst"
`)
	defer mockNrr.Restore()

	err := restart.FinishTaskWithRestart(s.t1, state.DoneStatus, restart.RestartSystem, "some-snap", nil)
	c.Check(err, DeepEquals, &state.Wait{Reason: "waiting for manual system restart"})
	c.Check(mockNrr.Calls(), DeepEquals, [][]string{
		{"notify-reboot-required", "snap:some-snap"},
	})
	// nothing in the logs
	c.Check(s.mockLog.String(), Equals, "")
}

func (s *notifyRebootRequiredSuite) TestFinishTaskWithRestartNotifiesRebootRequiredLogsErr(c *C) {
	s.st.Lock()
	defer s.st.Unlock()

	mockNrr := testutil.MockCommand(c, s.mockNrrPath, `echo fail; exit 1`)
	defer mockNrr.Restore()

	err := restart.FinishTaskWithRestart(s.t1, state.DoneStatus, restart.RestartSystem, "some-snap", nil)
	c.Check(err, DeepEquals, &state.Wait{Reason: "waiting for manual system restart"})
	c.Check(mockNrr.Calls(), DeepEquals, [][]string{
		{"notify-reboot-required", "snap:some-snap"},
	})
	// failures get logged
	c.Check(s.mockLog.String(), Matches, `(?ms).*: cannot notify about pending reboot: fail`)
	// and wait-boot-id is setup correctly
	var waitBootID string
	err = s.t1.Get("wait-for-system-restart-from-boot-id", &waitBootID)
	c.Check(err, IsNil)
	c.Check(waitBootID, Equals, "boot-id-1")
}

func (s *notifyRebootRequiredSuite) TestFinishTaskWithRestartNotifiesRebootRequiredNotOnCore(c *C) {
	restore := release.MockOnClassic(false)
	defer restore()

	s.st.Lock()
	defer s.st.Unlock()

	mockNrr := testutil.MockCommand(c, s.mockNrrPath, "")
	defer mockNrr.Restore()

	err := restart.FinishTaskWithRestart(s.t1, state.DoneStatus, restart.RestartSystem, "some-snap", nil)
	c.Check(err, IsNil)
	c.Check(mockNrr.Calls(), HasLen, 0)
	c.Check(s.mockLog.String(), Equals, "")
}
