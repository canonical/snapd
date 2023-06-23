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

func newRestartManager(c *C, st *state.State, bootID string, h restart.Handler) *restart.RestartManager {
	o := overlord.Mock()
	mgr, err := restart.Manager(st, o.TaskRunner(), "boot-id-1", h)
	c.Assert(err, IsNil)
	return mgr
}

func (s *restartSuite) TestManager(c *C) {
	st := state.New(nil)

	st.Lock()
	defer st.Unlock()

	mgr := newRestartManager(c, st, "boot-id-1", nil)
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

	newRestartManager(c, st, "boot-id-1", h)
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

	newRestartManager(c, st, "boot-id-1", nil)
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
	newRestartManager(c, st, "boot-id-1", h)
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
	newRestartManager(c, st, "boot-id-1", h1)
	c.Check(h1.rebootAsExpected, Equals, false)
	c.Check(h1.rebootDidNotHappen, Equals, true)
	fromBootID = ""
	c.Check(st.Get("system-restart-from-boot-id", &fromBootID), IsNil)
	c.Check(fromBootID, Equals, "boot-id-1")

	h2 := &testHandler{}
	newRestartManager(c, st, "boot-id-2", h2)
	c.Check(h2.rebootAsExpected, Equals, true)
	c.Check(st.Get("system-restart-from-boot-id", &fromBootID), testutil.ErrorIs, state.ErrNoState)
}

func (s *restartSuite) TestRequestRestartSystemWithRebootInfo(c *C) {
	st := state.New(nil)
	st.Lock()
	defer st.Unlock()

	h := &testHandler{}
	newRestartManager(c, st, "boot-id-1", h)
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
	newRestartManager(c, st, "boot-id-1", h1)
	c.Check(h1.rebootAsExpected, Equals, false)
	c.Check(h1.rebootDidNotHappen, Equals, true)
	fromBootID = ""
	c.Check(st.Get("system-restart-from-boot-id", &fromBootID), IsNil)
	c.Check(fromBootID, Equals, "boot-id-1")

	h2 := &testHandler{}
	newRestartManager(c, st, "boot-id-2", h2)
	c.Check(h2.rebootAsExpected, Equals, true)
	c.Check(st.Get("system-restart-from-boot-id", &fromBootID), testutil.ErrorIs, state.ErrNoState)
}

func (s *restartSuite) TestRequestRestartForTask(c *C) {
	st := state.New(nil)

	st.Lock()
	defer st.Unlock()

	defer release.MockOnClassic(false)()

	newRestartManager(c, st, "boot-id-1", nil)

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
		{initial: state.DoStatus, final: state.DoneStatus, restartType: restart.RestartSystem, classic: false, restart: true, log: ".* INFO System restart requested by snap \"some-snap\""},
		{initial: state.DoStatus, final: state.DoneStatus, restartType: restart.RestartSystem, classic: true, restart: false, wait: true, log: ".* INFO System restart requested by snap \"some-snap\""},
		{initial: state.DoStatus, final: state.DoneStatus, restartType: restart.RestartSystemNow, classic: true, restart: false, wait: true, log: ".* INFO System restart requested by snap \"some-snap\""},
		{initial: state.UndoStatus, final: state.UndoneStatus, restartType: restart.RestartSystem, classic: true, restart: false, log: ".* INFO Skipped automatic system restart on classic system when undoing changes back to previous state"},
		{initial: state.UndoStatus, final: state.UndoneStatus, restartType: restart.RestartSystem, classic: false, restart: true, log: ".* INFO System restart requested by snap \"some-snap\""},
	}

	waitCount := 0
	for _, t := range tests {
		restart.MockPending(st, restart.RestartUnset)
		release.MockOnClassic(t.classic)

		chg := st.NewChange("chg", "...")
		task := st.NewTask("foo", "...")
		chg.AddTask(task)
		task.SetStatus(t.initial)

		err := restart.RequestRestartForTask(task, "some-snap", t.final, t.restartType, nil)
		c.Check(err, IsNil)

		// For daemon restarts the logic is a bit simpler, as directly leads to the restart handler
		if t.restartType == restart.RestartDaemon {
			var waitBootID string
			if err := task.Get("wait-for-system-restart-from-boot-id", &waitBootID); !errors.Is(err, state.ErrNoState) {
				c.Check(err, IsNil)
			}

			ok, rst := restart.Pending(st)
			c.Check(task.Status(), Equals, t.final)
			c.Check(ok, Equals, true)
			c.Check(rst, Equals, restart.RestartDaemon)
			c.Check(waitBootID, Equals, "")
			continue
		}

		// For system restarts, we also call the RequestRestartForChange to
		// make it trigger the restart.Request
		if t.classic && t.final == state.UndoneStatus {
			c.Check(task.Status(), Equals, state.UndoneStatus)
		} else {
			c.Check(task.Status(), Equals, state.WaitStatus)
			c.Check(task.WaitedStatus(), Equals, t.final)
		}
		err = restart.RequestRestartForChange(chg)
		c.Check(err, IsNil)

		var waitBootID string
		if err := task.Get("wait-for-system-restart-from-boot-id", &waitBootID); !errors.Is(err, state.ErrNoState) {
			c.Check(err, IsNil)
		}

		ok, rst := restart.Pending(st)
		if t.restart {
			c.Check(ok, Equals, true)
			c.Check(rst, Equals, t.restartType)
			c.Check(waitBootID, Equals, "boot-id-1")
		} else {
			c.Check(ok, Equals, false)
			if t.wait {
				waitCount++
				c.Check(waitBootID, Equals, "boot-id-1")
				var wait bool
				c.Check(chg.Get("wait-for-system-restart", &wait), IsNil)
				c.Check(wait, Equals, waitCount != 0)
			} else {
				c.Check(waitBootID, Equals, "")
			}
		}

		if t.log == "" {
			c.Check(task.Log(), HasLen, 0)
		} else if t.classic && t.initial == state.UndoStatus {
			c.Check(task.Log(), HasLen, 2)
			c.Check(task.Log()[0], Matches, ".* INFO System restart requested by snap \"some-snap\"")
			c.Check(task.Log()[1], Matches, t.log)
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

	rm := newRestartManager(c, st, "boot-id-1", nil)

	chg := st.NewChange("chg", "...")
	t0 := st.NewTask("todo", "...")
	// needed in change otherwise the change is considered ready
	chg.AddTask(t0)

	t1 := st.NewTask("wait", "...")
	t1.SetToWait(state.DoneStatus)
	chg.AddTask(t1)

	t2 := st.NewTask("wait-for-reboot", "...")
	chg.AddTask(t2)
	err := restart.RequestRestartForTask(t2, "some-snap", state.DoneStatus, restart.RestartSystem, nil)
	c.Assert(err, FitsTypeOf, &state.Wait{WaitedStatus: state.DoneStatus})
	t2.SetToWait(state.DoneStatus)

	restart.ReplaceBootID(st, "boot-id-2")

	t3 := st.NewTask("wait-for-reboot-same-boot", "...")
	chg.AddTask(t3)
	err = restart.RequestRestartForTask(t3, "some-snap", state.DoneStatus, restart.RestartSystem, nil)
	c.Assert(err, FitsTypeOf, &state.Wait{WaitedStatus: state.DoneStatus})
	t3.SetToWait(state.DoneStatus)

	t4 := st.NewTask("do-after-wait", "...")
	t4.SetToWait(state.DoStatus)
	t4.Set("wait-for-system-restart-from-boot-id", "boot-id-2")
	chg.AddTask(t4)

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
	// same boot id in task/system, status does not change
	c.Check(t4.Status(), Equals, state.WaitStatus)

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
	// Should now have changed status
	c.Check(t4.Status(), Equals, state.DoStatus)

	c.Check(chg.Has("wait-for-system-restart"), Equals, false)
}

func (s *restartSuite) TestPendingForSystemRestartNoWaitTasks(c *C) {
	st := state.New(nil)

	st.Lock()
	defer st.Unlock()

	rm := newRestartManager(c, st, "boot-id-1", nil)

	chg1 := st.NewChange("not-ready", "...")
	t1 := st.NewTask("task", "...")
	chg1.AddTask(t1)
	c.Check(chg1.IsReady(), Equals, false)

	c.Check(rm.PendingForSystemRestart(chg1), Equals, false)
}

func (s *restartSuite) TestPendingForSystemRestartWaitTasksButNotPending(c *C) {
	release.MockOnClassic(false)
	st := state.New(nil)

	st.Lock()
	defer st.Unlock()

	rm := newRestartManager(c, st, "boot-id-1", nil)

	chg1 := st.NewChange("not-pending-do", "...")
	t1 := st.NewTask("wait-task", "...")
	t2 := st.NewTask("task", "...")
	t3 := st.NewTask("task", "...")
	t4 := st.NewTask("task", "...")
	chg1.AddTask(t1)
	chg1.AddTask(t2)
	chg1.AddTask(t3)
	chg1.AddTask(t4)
	t2.WaitFor(t1)
	t3.WaitFor(t2)
	t4.WaitFor(t2)

	// Requesting a reboot for task1 will put it's halt-tasks into Wait status, with their
	// WaitedStatus set to Do.
	err := restart.RequestRestartForTask(t1, "some-snap", state.DoneStatus, restart.RestartSystem, nil)
	c.Assert(err, IsNil)
	c.Check(t1.Status(), Equals, state.DoneStatus)
	c.Check(t2.Status(), Equals, state.WaitStatus)
	c.Check(t2.WaitedStatus(), Equals, state.DoStatus)

	// A change can't be pending if the tasks that are waiting, with completion statuses
	// set to 'Do'/'Done' have halt-tasks which are not set to 'Do'.
	t3.SetStatus(state.UndoStatus)
	t4.SetToWait(state.DoneStatus)
	c.Check(chg1.IsReady(), Equals, false)

	chg2 := st.NewChange("not-pending-undo", "...")
	t5 := st.NewTask("wait-task", "...")
	t6 := st.NewTask("task", "...")
	t7 := st.NewTask("task", "...")
	t8 := st.NewTask("task", "...")
	chg2.AddTask(t5)
	chg2.AddTask(t6)
	chg2.AddTask(t7)
	chg2.AddTask(t8)
	t7.WaitFor(t5)
	t7.WaitFor(t6)
	t8.WaitFor(t7)

	t5.SetStatus(state.UndoStatus)
	t6.SetStatus(state.UndoStatus)
	t7.SetStatus(state.UndoStatus)
	t8.SetStatus(state.UndoStatus)

	// Requesting a reboot for task8 will put it's halt-tasks into Wait status, with their
	// WaitedStatus set to Do.
	err = restart.RequestRestartForTask(t8, "some-snap", state.UndoneStatus, restart.RestartSystem, nil)
	c.Assert(err, IsNil)
	c.Check(t8.Status(), Equals, state.UndoneStatus)
	c.Check(t7.Status(), Equals, state.WaitStatus)
	c.Check(t7.WaitedStatus(), Equals, state.UndoStatus)

	// A change can't be pending if the tasks that are waiting, with completion statuses
	// set to 'Undo'/'Undone' have wait-tasks which are not set to 'Undo'.
	t5.SetStatus(state.DoStatus)
	t6.SetToWait(state.UndoneStatus)
	c.Check(chg1.IsReady(), Equals, false)

	c.Check(rm.PendingForSystemRestart(chg1), Equals, false)
	c.Check(rm.PendingForSystemRestart(chg2), Equals, false)
}

func (s *restartSuite) TestPendingForSystemRestartPending(c *C) {
	st := state.New(nil)

	st.Lock()
	defer st.Unlock()

	rm := newRestartManager(c, st, "boot-id-1", nil)

	chg1 := st.NewChange("pending", "...")
	chg1.Set("wait-for-system-restart", true)
	t1 := st.NewTask("wait-task", "...")
	t1.Set("wait-for-system-restart-from-boot-id", "boot-id-1")
	c.Check(t1.Status(), Equals, state.DoStatus)
	t1.SetToWait(state.DoneStatus)
	t2 := st.NewTask("task", "...")
	t3 := st.NewTask("task", "...")
	chg1.AddTask(t1)
	chg1.AddTask(t2)
	chg1.AddTask(t3)
	t2.WaitFor(t1)
	t3.WaitFor(t1)
	t3.SetStatus(state.DoStatus)
	c.Check(chg1.IsReady(), Equals, false)

	chg2 := st.NewChange("pending", "...")
	chg2.Set("wait-for-system-restart", true)
	t4 := st.NewTask("wait-task", "...")
	t4.Set("wait-for-system-restart-from-boot-id", "boot-id-1")
	c.Check(t4.Status(), Equals, state.DoStatus)
	t4.SetToWait(state.DoneStatus)
	chg2.AddTask(t4)
	c.Check(t4.Status(), Equals, state.WaitStatus)
	// nothing after t8
	c.Check(chg2.IsReady(), Equals, false)

	c.Check(rm.PendingForSystemRestart(chg1), Equals, true)
	c.Check(rm.PendingForSystemRestart(chg2), Equals, true)
}

type notifyRebootRequiredSuite struct {
	testutil.BaseTest

	st          *state.State
	mockNrrPath string
	mockLog     *bytes.Buffer
	t1          *state.Task
	chg         *state.Change
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

	newRestartManager(c, s.st, "boot-id-1", nil)

	// pretend there is a snap that requires a reboot
	s.chg = s.st.NewChange("not-ready", "...")
	s.t1 = s.st.NewTask("task", "...")
	s.chg.AddTask(s.t1)
}

func (s *notifyRebootRequiredSuite) TestRequestRestartForTaskNotifiesRebootRequired(c *C) {
	s.st.Lock()
	defer s.st.Unlock()

	mockNrr := testutil.MockCommand(c, s.mockNrrPath, `
test "$DPKG_MAINTSCRIPT_PACAGE" = "snap:some-snap"
test "$DPKG_MAINTSCRIPT_NAME" = "postinst"
`)
	defer mockNrr.Restore()

	err := restart.RequestRestartForTask(s.t1, "some-snap", state.DoneStatus, restart.RestartSystem, nil)
	c.Assert(err, IsNil)

	err = restart.RequestRestartForChange(s.chg)
	c.Assert(err, IsNil)

	c.Check(mockNrr.Calls(), DeepEquals, [][]string{
		{"notify-reboot-required", "snap:some-snap"},
	})
	c.Check(s.mockLog.String(), Matches, ".* Postponing restart until a manual system restart allows to continue\n")
}

func (s *notifyRebootRequiredSuite) TestRequestRestartForTaskNotifiesRebootRequiredLogsErr(c *C) {
	s.st.Lock()
	defer s.st.Unlock()

	mockNrr := testutil.MockCommand(c, s.mockNrrPath, `echo fail; exit 1`)
	defer mockNrr.Restore()

	err := restart.RequestRestartForTask(s.t1, "some-snap", state.DoneStatus, restart.RestartSystem, nil)
	c.Assert(err, IsNil)

	err = restart.RequestRestartForChange(s.chg)
	c.Assert(err, IsNil)

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

func (s *notifyRebootRequiredSuite) TestRequestRestartForTaskNotifiesRebootRequiredNotOnCore(c *C) {
	restore := release.MockOnClassic(false)
	defer restore()

	s.st.Lock()
	defer s.st.Unlock()

	mockNrr := testutil.MockCommand(c, s.mockNrrPath, "")
	defer mockNrr.Restore()

	err := restart.RequestRestartForTask(s.t1, "some-snap", state.DoneStatus, restart.RestartSystem, nil)
	c.Check(err, IsNil)
	c.Check(mockNrr.Calls(), HasLen, 0)
	c.Check(s.mockLog.String(), Equals, "")
}
