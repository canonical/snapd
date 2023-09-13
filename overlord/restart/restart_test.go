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
	"github.com/snapcore/snapd/bootloader"
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
		RebootRequired:    true,
		BootloaderOptions: &bootloader.Options{},
	})

	c.Check(h.restartRequested, Equals, true)
	c.Check(h.rebootInfo.RebootRequired, Equals, true)
	c.Check(h.rebootInfo.BootloaderOptions, NotNil)

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
			// WaitStatus is driven by taskrunner, so if the task is waiting then
			// it still has it's initial state.
			setStatus = t.initial
			c.Check(err, FitsTypeOf, &state.Wait{WaitedStatus: state.DoneStatus})
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
				c.Check(err, DeepEquals, &state.Wait{Reason: "waiting for manual system restart", WaitedStatus: state.DoneStatus})
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

func (s *restartSuite) TestProcessRestartForChangeClassic(c *C) {
	buf, restore := logger.MockLogger()
	defer restore()
	restore = release.MockOnClassic(true)
	defer restore()

	st := state.New(nil)
	st.Lock()
	defer st.Unlock()

	_, err := restart.Manager(st, "boot-id-1", nil)
	c.Assert(err, IsNil)

	chg := st.NewChange("test", "...")
	t := st.NewTask("waiting", "...")

	chg.AddTask(t)

	restart.MarkTaskAsRestartBoundary(t, restart.RestartBoundaryDirectionDo)

	err = restart.FinishTaskWithRestart2(t, "some-snap", state.DoneStatus, restart.RestartSystem, nil)
	c.Assert(err, IsNil)
	c.Check(t.Status(), Equals, state.WaitStatus)
	c.Check(t.WaitedStatus(), Equals, state.DoneStatus)

	err = restart.ProcessRestartForChange(chg)
	c.Assert(err, IsNil)

	// ensure restart-ctx was cleared
	var rt restart.RestartParameters
	c.Check(chg.Get("pending-system-restart", &rt), FitsTypeOf, &state.NoStateError{})

	c.Assert(buf.String(), testutil.Contains, `Postponing restart until a manual system restart allows to continue`)
}

func (s *restartSuite) TestProcessRestartForChangeCore(c *C) {
	restore := release.MockOnClassic(false)
	defer restore()

	st := state.New(nil)
	st.Lock()
	defer st.Unlock()

	h := &testHandler{}
	_, err := restart.Manager(st, "boot-id-1", h)
	c.Assert(err, IsNil)

	chg := st.NewChange("test", "...")
	t := st.NewTask("waiting", "...")

	chg.AddTask(t)

	restart.MarkTaskAsRestartBoundary(t, restart.RestartBoundaryDirectionDo)

	err = restart.FinishTaskWithRestart2(t, "some-snap", state.DoneStatus, restart.RestartSystem, nil)
	c.Assert(err, IsNil)
	c.Check(t.Status(), Equals, state.WaitStatus)
	c.Check(t.WaitedStatus(), Equals, state.DoneStatus)

	err = restart.ProcessRestartForChange(chg)
	c.Assert(err, IsNil)
	c.Check(h.restartRequested, Equals, true)
	c.Assert(h.rebootInfo, NotNil)
	c.Check(h.rebootInfo.RebootRequired, Equals, true)
}

func (s *restartSuite) TestProcessRestartForChangeMissingRebootContext(c *C) {
	st := state.New(nil)
	st.Lock()
	defer st.Unlock()

	chg := st.NewChange("test", "...")
	t := st.NewTask("waiting", "...")

	chg.AddTask(t)
	t.SetToWait(state.DoneStatus)

	err := restart.ProcessRestartForChange(chg)
	c.Assert(err, ErrorMatches, `change 1 is waiting to continue but has not requested any reboots`)
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
	t1.SetToWait(state.DoneStatus)
	chg.AddTask(t1)

	t2 := st.NewTask("wait-for-reboot", "...")
	chg.AddTask(t2)
	err = restart.FinishTaskWithRestart(t2, state.DoneStatus, restart.RestartSystem, "some-snap", nil)
	c.Assert(err, FitsTypeOf, &state.Wait{WaitedStatus: state.DoneStatus})
	t2.SetToWait(state.DoneStatus)

	restart.ReplaceBootID(st, "boot-id-2")

	t3 := st.NewTask("wait-for-reboot-same-boot", "...")
	chg.AddTask(t3)
	err = restart.FinishTaskWithRestart(t3, state.DoneStatus, restart.RestartSystem, "some-snap", nil)
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

func (s *restartSuite) TestPendingForChange(c *C) {
	st := state.New(nil)

	st.Lock()
	defer st.Unlock()

	_, err := restart.Manager(st, "boot-id-1", nil)
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
	c.Assert(err, FitsTypeOf, &state.Wait{WaitedStatus: state.DoneStatus})
	t3.SetStatus(state.UndoStatus)
	t4.SetToWait(state.DoneStatus)
	c.Check(chg2.IsReady(), Equals, false)

	chg3 := st.NewChange("pending", "...")
	chg3.Set("wait-for-system-restart", true)
	t5 := st.NewTask("wait-task", "...")
	t5.Set("wait-for-system-restart-from-boot-id", "boot-id-1")
	c.Check(t5.Status(), Equals, state.DoStatus)
	t5.SetToWait(state.DoneStatus)
	t6 := st.NewTask("task", "...")
	t7 := st.NewTask("task", "...")
	chg3.AddTask(t5)
	chg3.AddTask(t6)
	chg3.AddTask(t7)
	t6.WaitFor(t5)
	t7.WaitFor(t5)
	t7.SetStatus(state.DoStatus)
	c.Check(chg3.IsReady(), Equals, false)

	chg4 := st.NewChange("pending", "...")
	chg4.Set("wait-for-system-restart", true)
	t8 := st.NewTask("wait-task", "...")
	t8.Set("wait-for-system-restart-from-boot-id", "boot-id-1")
	c.Check(t8.Status(), Equals, state.DoStatus)
	t8.SetToWait(state.DoneStatus)
	chg4.AddTask(t8)
	c.Check(t8.Status(), Equals, state.WaitStatus)
	// nothing after t8
	c.Check(chg4.IsReady(), Equals, false)

	c.Check(restart.PendingForChange(st, chg1), Equals, false)
	c.Check(restart.PendingForChange(st, chg2), Equals, false)
	c.Check(restart.PendingForChange(st, chg3), Equals, true)
	c.Check(restart.PendingForChange(st, chg4), Equals, true)
}

func (s *restartSuite) TestMarkTaskForRestart(c *C) {
	st := state.New(nil)
	st.Lock()
	defer st.Unlock()

	_, err := restart.Manager(st, "boot-id-1", nil)
	c.Assert(err, IsNil)

	chg := st.NewChange("test", "...")
	t1 := st.NewTask("foo", "...")
	chg.AddTask(t1)

	restart.MarkTaskForRestart(t1, state.DoneStatus, true)

	var waitBootID string
	if err := t1.Get("wait-for-system-restart-from-boot-id", &waitBootID); !errors.Is(err, state.ErrNoState) {
		c.Check(err, IsNil)
	}
	c.Check(waitBootID, Equals, "boot-id-1")
	c.Check(t1.Status(), Equals, state.WaitStatus)
	c.Check(t1.WaitedStatus(), Equals, state.DoneStatus)
}

func (s *restartSuite) TestMarkTaskForRestartClassicUndo(c *C) {
	restore := release.MockOnClassic(true)
	defer restore()

	st := state.New(nil)
	st.Lock()
	defer st.Unlock()

	chg := st.NewChange("test", "...")
	t1 := st.NewTask("foo", "...")
	chg.AddTask(t1)

	restart.MarkTaskForRestart(t1, state.UndoneStatus, true)

	var waitBootID string
	if err := t1.Get("wait-for-system-restart-from-boot-id", &waitBootID); !errors.Is(err, state.ErrNoState) {
		c.Check(err, IsNil)
	}
	c.Check(waitBootID, Equals, "")
	c.Check(t1.Status(), Equals, state.UndoneStatus)
	c.Assert(t1.Log(), HasLen, 1)
	c.Check(t1.Log()[0], Matches, `.* Skipped automatic system restart on classic system when undoing changes back to previous state`)
}

func (s *restartSuite) TestTaskWaitForRestartDo(c *C) {
	st := state.New(nil)
	st.Lock()
	defer st.Unlock()

	_, err := restart.Manager(st, "boot-id-1", nil)
	c.Assert(err, IsNil)

	chg := st.NewChange("test", "...")
	t1 := st.NewTask("foo", "...")
	chg.AddTask(t1)

	t1.SetStatus(state.DoingStatus)

	err = restart.TaskWaitForRestart(t1)
	c.Assert(err, IsNil)

	c.Check(t1.Log(), HasLen, 1)
	c.Check(t1.Log()[0], Matches, ".* Task set to wait until a system restart allows to continue")

	var waitBootID string
	if err := t1.Get("wait-for-system-restart-from-boot-id", &waitBootID); !errors.Is(err, state.ErrNoState) {
		c.Check(err, IsNil)
	}
	c.Check(waitBootID, Equals, "boot-id-1")
	c.Check(t1.Status(), Equals, state.WaitStatus)
	c.Check(t1.WaitedStatus(), Equals, state.DoStatus)

	c.Check(t1.Log(), HasLen, 1)
	c.Check(t1.Log()[0], Matches, ".* Task set to wait until a system restart allows to continue")
}

func (s *restartSuite) TestTaskWaitForRestartUndoClassic(c *C) {
	restore := release.MockOnClassic(true)
	defer restore()

	st := state.New(nil)
	st.Lock()
	defer st.Unlock()

	chg := st.NewChange("test", "...")
	t1 := st.NewTask("foo", "...")
	chg.AddTask(t1)

	t1.SetStatus(state.UndoingStatus)

	err := restart.TaskWaitForRestart(t1)
	c.Assert(err, IsNil)

	c.Check(t1.Log(), HasLen, 1)
	c.Check(t1.Log()[0], Matches, ".* Skipped automatic system restart on classic system when undoing changes back to previous state")
}

func (s *restartSuite) TestTaskWaitForRestartUndoCore(c *C) {
	restore := release.MockOnClassic(false)
	defer restore()

	st := state.New(nil)
	st.Lock()
	defer st.Unlock()

	_, err := restart.Manager(st, "boot-id-1", nil)
	c.Assert(err, IsNil)

	chg := st.NewChange("test", "...")
	t1 := st.NewTask("foo", "...")
	chg.AddTask(t1)

	t1.SetStatus(state.UndoingStatus)

	err = restart.TaskWaitForRestart(t1)
	c.Assert(err, IsNil)

	c.Check(t1.Log(), HasLen, 1)
	c.Check(t1.Log()[0], Matches, ".* Task set to wait until a system restart allows to continue")

	var waitBootID string
	if err := t1.Get("wait-for-system-restart-from-boot-id", &waitBootID); !errors.Is(err, state.ErrNoState) {
		c.Check(err, IsNil)
	}
	c.Check(waitBootID, Equals, "boot-id-1")
	c.Check(t1.Status(), Equals, state.WaitStatus)
	c.Check(t1.WaitedStatus(), Equals, state.UndoStatus)

	c.Check(t1.Log(), HasLen, 1)
	c.Check(t1.Log()[0], Matches, ".* Task set to wait until a system restart allows to continue")
}

func (s *restartSuite) TestTaskWaitForRestartInvalid(c *C) {
	st := state.New(nil)
	st.Lock()
	defer st.Unlock()

	chg := st.NewChange("test", "...")
	t1 := st.NewTask("foo", "...")
	chg.AddTask(t1)

	err := restart.TaskWaitForRestart(t1)
	c.Assert(err, ErrorMatches, `internal error: only tasks currently in progress \(doing/undoing\) are supported`)
}

func (s *restartSuite) TestFinishTaskWithRestart2Done(c *C) {
	st := state.New(nil)
	st.Lock()
	defer st.Unlock()

	_, err := restart.Manager(st, "boot-id-1", nil)
	c.Assert(err, IsNil)

	chg := st.NewChange("test", "...")
	t := st.NewTask("waiting", "...")
	chg.AddTask(t)

	err = restart.FinishTaskWithRestart2(t, "some-snap", state.DoneStatus, restart.RestartSystemNow, nil)
	c.Assert(err, IsNil)
	c.Check(t.Status(), Equals, state.DoneStatus)
	c.Check(chg.Status(), Equals, state.DoneStatus)

	// Restart must still be requested
	rt, err := restart.RestartParametersFromChange(chg)
	c.Assert(err, IsNil)
	c.Check(rt.SnapName, Equals, "some-snap")
	c.Check(rt.RestartType, Equals, restart.RestartSystemNow)
	c.Check(rt.BootloaderOptions, IsNil)
}

func (s *restartSuite) TestFinishTaskWithRestart2DoneWithRestartBoundary(c *C) {
	st := state.New(nil)
	st.Lock()
	defer st.Unlock()

	_, err := restart.Manager(st, "boot-id-1", nil)
	c.Assert(err, IsNil)

	chg := st.NewChange("test", "...")
	t := st.NewTask("waiting", "...")
	chg.AddTask(t)

	restart.MarkTaskAsRestartBoundary(t, restart.RestartBoundaryDirectionDo)

	err = restart.FinishTaskWithRestart2(t, "some-snap", state.DoneStatus, restart.RestartSystemNow, nil)
	c.Assert(err, IsNil)
	c.Check(t.Status(), Equals, state.WaitStatus)
	c.Check(t.WaitedStatus(), Equals, state.DoneStatus)

	rt, err := restart.RestartParametersFromChange(chg)
	c.Assert(err, IsNil)
	c.Check(rt.SnapName, Equals, "some-snap")
	c.Check(rt.RestartType, Equals, restart.RestartSystemNow)
	c.Check(rt.BootloaderOptions, IsNil)
}

func (s *restartSuite) TestFinishTaskWithRestart2WithArguments(c *C) {
	st := state.New(nil)
	st.Lock()
	defer st.Unlock()

	_, err := restart.Manager(st, "boot-id-1", nil)
	c.Assert(err, IsNil)

	chg := st.NewChange("test", "...")
	t := st.NewTask("waiting", "...")
	chg.AddTask(t)

	restart.MarkTaskAsRestartBoundary(t, restart.RestartBoundaryDirectionDo)

	err = restart.FinishTaskWithRestart2(t, "some-snap", state.DoneStatus, restart.RestartSystemNow,
		&boot.RebootInfo{
			BootloaderOptions: &bootloader.Options{
				PrepareImageTime: true,
				Role:             bootloader.RoleRunMode,
			},
		})
	c.Assert(err, IsNil)
	c.Check(t.Status(), Equals, state.WaitStatus)
	c.Check(t.WaitedStatus(), Equals, state.DoneStatus)

	rt, err := restart.RestartParametersFromChange(chg)
	c.Assert(err, IsNil)
	c.Check(rt.SnapName, Equals, "some-snap")
	c.Check(rt.RestartType, Equals, restart.RestartSystemNow)
	c.Check(rt.BootloaderOptions, DeepEquals, &bootloader.Options{
		PrepareImageTime: true,
		Role:             bootloader.RoleRunMode,
	})
}

func (s *restartSuite) TestFinishTaskWithRestart2UndoneClassic(c *C) {
	restore := release.MockOnClassic(true)
	defer restore()

	st := state.New(nil)
	st.Lock()
	defer st.Unlock()

	_, err := restart.Manager(st, "boot-id-1", nil)
	c.Assert(err, IsNil)

	chg := st.NewChange("test", "...")
	t := st.NewTask("waiting", "...")

	chg.AddTask(t)

	err = restart.FinishTaskWithRestart2(t, "some-snap", state.UndoneStatus, restart.RestartSystemNow, nil)
	c.Assert(err, IsNil)
	c.Check(t.Status(), Equals, state.UndoneStatus)

	rt, err := restart.RestartParametersFromChange(chg)
	c.Assert(err, IsNil)
	c.Check(rt.SnapName, Equals, "some-snap")
	c.Check(rt.RestartType, Equals, restart.RestartSystemNow)
	c.Check(rt.BootloaderOptions, IsNil)

	c.Check(t.Log(), HasLen, 1)
	c.Check(t.Log()[0], Matches, ".* Skipped automatic system restart on classic system when undoing changes back to previous state")
}

func (s *restartSuite) TestFinishTaskWithRestart2UndoneCore(c *C) {
	restore := release.MockOnClassic(false)
	defer restore()

	st := state.New(nil)
	st.Lock()
	defer st.Unlock()

	_, err := restart.Manager(st, "boot-id-1", nil)
	c.Assert(err, IsNil)

	chg := st.NewChange("test", "...")
	t := st.NewTask("waiting", "...")

	chg.AddTask(t)

	err = restart.FinishTaskWithRestart2(t, "some-snap", state.UndoneStatus, restart.RestartSystemNow, nil)
	c.Assert(err, IsNil)
	c.Check(t.Status(), Equals, state.UndoneStatus)
	c.Check(chg.Status(), Equals, state.UndoneStatus)

	// Restart must still be requested
	rt, err := restart.RestartParametersFromChange(chg)
	c.Assert(err, IsNil)
	c.Check(rt.SnapName, Equals, "some-snap")
	c.Check(rt.RestartType, Equals, restart.RestartSystemNow)
	c.Check(rt.BootloaderOptions, IsNil)
}

func (s *restartSuite) TestFinishTaskWithRestart2UndoneCoreWithRestartBoundary(c *C) {
	restore := release.MockOnClassic(false)
	defer restore()

	st := state.New(nil)
	st.Lock()
	defer st.Unlock()

	_, err := restart.Manager(st, "boot-id-1", nil)
	c.Assert(err, IsNil)

	chg := st.NewChange("test", "...")
	t := st.NewTask("waiting", "...")

	chg.AddTask(t)

	restart.MarkTaskAsRestartBoundary(t, restart.RestartBoundaryDirectionUndo)

	err = restart.FinishTaskWithRestart2(t, "some-snap", state.UndoneStatus, restart.RestartSystemNow, nil)
	c.Assert(err, IsNil)
	c.Check(t.Status(), Equals, state.WaitStatus)
	c.Check(t.WaitedStatus(), Equals, state.UndoneStatus)

	rt, err := restart.RestartParametersFromChange(chg)
	c.Assert(err, IsNil)
	c.Check(rt.SnapName, Equals, "some-snap")
	c.Check(rt.RestartType, Equals, restart.RestartSystemNow)
	c.Check(rt.BootloaderOptions, IsNil)
}

func (s *restartSuite) TestFinishTaskWithRestart2Invalid(c *C) {
	st := state.New(nil)
	st.Lock()
	defer st.Unlock()

	chg := st.NewChange("test", "...")
	t := st.NewTask("waiting", "...")

	chg.AddTask(t)

	err := restart.FinishTaskWithRestart2(t, "", state.DoingStatus, restart.RestartSystem, nil)
	c.Assert(err, ErrorMatches, `internal error: unexpected task status when requesting system restart for task: Doing`)
}

func (s *restartSuite) TestRestartBoundaryDirectionMarshalJSON(c *C) {
	tests := []struct {
		value restart.RestartBoundaryDirection
		str   string
	}{
		{restart.RestartBoundaryDirection(0), `""`},
		{restart.RestartBoundaryDirection(32), `""`},
		{restart.RestartBoundaryDirectionDo, `"do"`},
		{restart.RestartBoundaryDirectionUndo, `"undo"`},
		{restart.RestartBoundaryDirectionDo | restart.RestartBoundaryDirectionUndo, `"do|undo"`},
	}

	for _, t := range tests {
		data, err := t.value.MarshalJSON()
		c.Check(err, IsNil)
		c.Check(string(data), Equals, t.str)
	}
}

func (s *restartSuite) TestRestartBoundaryDirectionUnmarshalJSON(c *C) {
	tests := []struct {
		str   string
		value restart.RestartBoundaryDirection
	}{
		{`""`, restart.RestartBoundaryDirection(0)},
		{`"i9045934"`, restart.RestartBoundaryDirection(0)},
		{`"do,undo"`, restart.RestartBoundaryDirection(0)},
		{`"do"`, restart.RestartBoundaryDirectionDo},
		{`"undo"`, restart.RestartBoundaryDirectionUndo},
		{`"do|undo"`, restart.RestartBoundaryDirectionDo | restart.RestartBoundaryDirectionUndo},
	}

	for _, t := range tests {
		var rbd restart.RestartBoundaryDirection
		c.Check(rbd.UnmarshalJSON([]byte(t.str)), IsNil)
		c.Check(rbd, Equals, t.value)
	}
}

func (s *restartSuite) TestMarkTaskAsRestartBoundarySimple(c *C) {
	st := state.New(nil)
	st.Lock()
	defer st.Unlock()

	t := st.NewTask("waiting", "...")
	restart.MarkTaskAsRestartBoundary(t, restart.RestartBoundaryDirectionDo)

	var boundary restart.RestartBoundaryDirection
	c.Check(t.Get("restart-boundary", &boundary), IsNil)
	c.Check(boundary, Equals, restart.RestartBoundaryDirectionDo)
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
	c.Check(err, DeepEquals, &state.Wait{Reason: "waiting for manual system restart", WaitedStatus: state.DoneStatus})
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
	c.Check(err, DeepEquals, &state.Wait{Reason: "waiting for manual system restart", WaitedStatus: state.DoneStatus})
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
