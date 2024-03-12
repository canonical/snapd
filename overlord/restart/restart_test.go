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
	restartType        restart.RestartType
	rebootAsExpected   bool
	rebootDidNotHappen bool
	rebootInfo         *boot.RebootInfo
}

func (h *testHandler) HandleRestart(t restart.RestartType, rbi *boot.RebootInfo) {
	h.restartRequested = true
	h.restartType = t
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
		{initial: state.DoStatus, final: state.DoneStatus, restartType: restart.RestartSystem, classic: false, restart: true, log: ".* INFO Task set to wait until a system restart allows to continue"},
		{initial: state.DoStatus, final: state.DoneStatus, restartType: restart.RestartSystem, classic: true, restart: false, wait: true, log: ".* INFO Task set to wait until a system restart allows to continue"},
		{initial: state.DoStatus, final: state.DoneStatus, restartType: restart.RestartSystemNow, classic: true, restart: false, wait: true, log: ".* INFO Task set to wait until a system restart allows to continue"},
		{initial: state.UndoStatus, final: state.UndoneStatus, restartType: restart.RestartSystem, classic: true, restart: false, log: ".* INFO Skipped automatic system restart on classic system when undoing changes back to previous state"},
		{initial: state.UndoStatus, final: state.UndoneStatus, restartType: restart.RestartSystem, classic: false, restart: true, log: ".* INFO Task set to wait until a system restart allows to continue"},
	}

	for _, t := range tests {
		restart.MockPending(st, restart.RestartUnset)
		release.MockOnClassic(t.classic)

		chg := st.NewChange("chg", "...")
		task := st.NewTask("foo", "...")
		chg.AddTask(task)
		task.SetStatus(t.initial)

		if t.restart {
			if t.initial == state.DoStatus {
				restart.MarkTaskAsRestartBoundary(task, restart.RestartBoundaryDirectionDo)
			} else {
				restart.MarkTaskAsRestartBoundary(task, restart.RestartBoundaryDirectionUndo)
			}
		}

		err := restart.FinishTaskWithRestart(task, t.final, t.restartType, "some-snap", nil)
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

		// For system restarts, we also call the ProcessRestartForChange to
		// make it trigger the restart.Request
		if t.classic && t.final == state.UndoneStatus {
			c.Check(task.Status(), Equals, state.UndoneStatus)
		} else {
			c.Check(task.Status(), Equals, state.WaitStatus)
			c.Check(task.WaitedStatus(), Equals, t.final)
			var waitBootID string
			if err := task.Get("wait-for-system-restart-from-boot-id", &waitBootID); !errors.Is(err, state.ErrNoState) {
				c.Check(err, IsNil)
			}
			c.Check(waitBootID, Equals, "boot-id-1")
		}
		restart.ProcessRestartForChange(chg, state.DefaultStatus, state.WaitStatus)

		ok, rst := restart.Pending(st)
		if t.restart {
			c.Check(ok, Equals, true)
			c.Check(rst, Equals, t.restartType)
		} else {
			c.Check(ok, Equals, false)

			var wait bool
			if err := chg.Get("wait-for-system-restart", &wait); !errors.Is(err, state.ErrNoState) {
				c.Check(err, IsNil)
			}
			c.Check(wait, Equals, t.wait)
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

	err = restart.FinishTaskWithRestart(t, state.DoneStatus, restart.RestartSystem, "some-snap", nil)
	c.Assert(err, IsNil)
	c.Check(t.Status(), Equals, state.WaitStatus)
	c.Check(t.WaitedStatus(), Equals, state.DoneStatus)

	restart.ProcessRestartForChange(chg, state.DefaultStatus, state.WaitStatus)

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

	err = restart.FinishTaskWithRestart(t, state.DoneStatus, restart.RestartSystem, "some-snap", nil)
	c.Assert(err, IsNil)
	c.Check(t.Status(), Equals, state.WaitStatus)
	c.Check(t.WaitedStatus(), Equals, state.DoneStatus)

	restart.ProcessRestartForChange(chg, state.DefaultStatus, state.WaitStatus)
	c.Check(h.restartRequested, Equals, true)
	c.Assert(h.rebootInfo, NotNil)
	c.Check(h.rebootInfo.RebootRequired, Equals, true)
}

func (s *restartSuite) TestProcessRestartForChangeMissingRebootContext(c *C) {
	ml, restore := logger.MockLogger()
	defer restore()

	st := state.New(nil)
	st.Lock()
	defer st.Unlock()

	chg := st.NewChange("test", "...")
	t := st.NewTask("waiting", "...")

	chg.AddTask(t)
	t.SetToWait(state.DoneStatus)

	restart.ProcessRestartForChange(chg, state.DefaultStatus, state.WaitStatus)
	c.Check(ml.String(), Matches, `.* change 1 is waiting to continue but failed to get parameters for reboot: no state entry for key \"pending-system-restart\"\n`)
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
	restart.MarkTaskAsRestartBoundary(t2, restart.RestartBoundaryDirectionDo)
	chg.AddTask(t2)
	err = restart.FinishTaskWithRestart(t2, state.DoneStatus, restart.RestartSystem, "some-snap", nil)
	c.Assert(err, IsNil)

	restart.ReplaceBootID(st, "boot-id-2")

	t3 := st.NewTask("wait-for-reboot-same-boot", "...")
	restart.MarkTaskAsRestartBoundary(t3, restart.RestartBoundaryDirectionDo)
	chg.AddTask(t3)
	err = restart.FinishTaskWithRestart(t3, state.DoneStatus, restart.RestartSystem, "some-snap", nil)
	c.Assert(err, IsNil)

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

func (s *restartSuite) TestStop(c *C) {
	restore := release.MockOnClassic(false)
	defer restore()

	st := state.New(nil)

	st.Lock()
	defer st.Unlock()

	h := &testHandler{}
	rm, err := restart.Manager(st, "boot-id-1", h)
	c.Assert(err, IsNil)
	se := overlord.NewStateEngine(st)
	se.AddManager(rm)
	st.Unlock()
	err = se.StartUp()
	st.Lock()
	c.Assert(err, IsNil)

	// At this point in time the restart manager should be listening
	// to events from state
	chg1 := st.NewChange("test-one", "...")
	t1 := st.NewTask("waiting", "...")
	chg1.AddTask(t1)

	err = restart.FinishTaskWithRestart(t1, state.DoneStatus, restart.RestartSystemNow, "some-snap", nil)
	c.Assert(err, IsNil)
	c.Check(t1.Status(), Equals, state.WaitStatus)
	c.Check(t1.WaitedStatus(), Equals, state.DoneStatus)
	c.Check(chg1.Status(), Equals, state.WaitStatus)

	// Expect the handler to have fired
	c.Check(h.restartRequested, Equals, true)
	c.Check(h.restartType, Equals, restart.RestartSystemNow)
	c.Check(h.rebootInfo.BootloaderOptions, IsNil)

	// Reset data
	h.restartRequested = false
	h.restartType = restart.RestartUnset

	// Now we stop it, and create a new change, and verify nothing happened
	st.Unlock()
	se.Stop()
	st.Lock()

	chg2 := st.NewChange("test-two", "...")
	t2 := st.NewTask("waiting", "...")
	chg2.AddTask(t2)

	err = restart.FinishTaskWithRestart(t2, state.DoneStatus, restart.RestartSystemNow, "some-snap", nil)
	c.Assert(err, IsNil)
	// Change has indeed changed status
	c.Check(t2.Status(), Equals, state.WaitStatus)
	c.Check(t2.WaitedStatus(), Equals, state.DoneStatus)
	c.Check(chg2.Status(), Equals, state.WaitStatus)

	// The handler should not have been invoked
	c.Check(h.restartRequested, Equals, false)
	c.Check(h.restartType, Equals, restart.RestartUnset)
	c.Check(h.rebootInfo.BootloaderOptions, IsNil)
}

func (s *restartSuite) TestPendingForChange(c *C) {
	st := state.New(nil)

	st.Lock()
	defer st.Unlock()

	_, err := restart.Manager(st, "boot-id-1", nil)
	c.Assert(err, IsNil)

	chg1 := st.NewChange("pending", "...")
	chg1.Set("wait-for-system-restart", true)
	t1 := st.NewTask("task", "...")
	chg1.AddTask(t1)
	t1.SetToWait(state.DoneStatus)
	t1.Set("wait-for-system-restart-from-boot-id", "boot-id-1")

	chg2 := st.NewChange("pending", "...")
	chg2.Set("wait-for-system-restart", true)
	t2 := st.NewTask("task", "...")
	chg2.AddTask(t2)
	t2.SetToWait(state.UndoneStatus)
	t2.Set("wait-for-system-restart-from-boot-id", "boot-id-1")

	c.Check(restart.PendingForChange(st, chg1), Equals, true)
	c.Check(restart.PendingForChange(st, chg2), Equals, true)
}

func (s *restartSuite) TestPendingForChangeWaitTasks(c *C) {
	st := state.New(nil)

	st.Lock()
	defer st.Unlock()

	_, err := restart.Manager(st, "boot-id-1", nil)
	c.Assert(err, IsNil)

	chg1 := st.NewChange("pending", "...")
	chg1.Set("wait-for-system-restart", true)
	t1 := st.NewTask("waiting", "...")
	t2 := st.NewTask("task-2", "...")
	t2.WaitFor(t1)
	chg1.AddTask(t1)
	chg1.AddTask(t2)
	t1.SetToWait(state.DoneStatus)
	t1.Set("wait-for-system-restart-from-boot-id", "boot-id-1")

	chg2 := st.NewChange("pending", "...")
	chg2.Set("wait-for-system-restart", true)
	t3 := st.NewTask("task", "...")
	t4 := st.NewTask("waiting", "...")
	t4.WaitFor(t3)
	chg2.AddTask(t3)
	chg2.AddTask(t4)
	t3.SetStatus(state.UndoStatus)
	t4.SetToWait(state.UndoneStatus)
	t4.Set("wait-for-system-restart-from-boot-id", "boot-id-1")

	c.Check(restart.PendingForChange(st, chg1), Equals, true)
	c.Check(restart.PendingForChange(st, chg2), Equals, true)
}

func (s *restartSuite) TestPendingForChangeNoWaitTasks(c *C) {
	st := state.New(nil)

	st.Lock()
	defer st.Unlock()

	_, err := restart.Manager(st, "boot-id-1", nil)
	c.Assert(err, IsNil)

	chg1 := st.NewChange("not-ready", "...")
	t1 := st.NewTask("task", "...")
	chg1.AddTask(t1)
	c.Check(chg1.IsReady(), Equals, false)

	c.Check(restart.PendingForChange(st, chg1), Equals, false)
}

func (s *restartSuite) TestPendingForChangeWaitTasksButNotPending(c *C) {
	release.MockOnClassic(false)
	st := state.New(nil)

	st.Lock()
	defer st.Unlock()

	_, err := restart.Manager(st, "boot-id-1", nil)
	c.Assert(err, IsNil)

	chg1 := st.NewChange("not-pending-do", "...")
	t1 := st.NewTask("wait-task", "...")
	t2 := st.NewTask("task", "...")
	chg1.AddTask(t1)
	chg1.AddTask(t2)
	t2.WaitFor(t1)

	restart.MarkTaskAsRestartBoundary(t1, restart.RestartBoundaryDirectionDo)

	err = restart.FinishTaskWithRestart(t1, state.DoneStatus, restart.RestartSystem, "some-snap", nil)
	c.Assert(err, IsNil)
	c.Check(t1.Status(), Equals, state.WaitStatus)
	c.Check(t1.WaitedStatus(), Equals, state.DoneStatus)
	c.Check(t2.Status(), Equals, state.DoStatus)

	// A change can't be pending if the tasks that are waiting, with completion statuses
	// set to 'Do'/'Done' have halt-tasks which are not set to 'Do'.
	t2.SetStatus(state.UndoStatus)
	c.Check(chg1.IsReady(), Equals, false)
	c.Check(restart.PendingForChange(st, chg1), Equals, false)

	chg2 := st.NewChange("not-pending-undo", "...")
	t3 := st.NewTask("task7", "...")
	t4 := st.NewTask("task8", "...")
	chg2.AddTask(t3)
	chg2.AddTask(t4)
	t4.WaitFor(t3)

	t3.SetStatus(state.UndoStatus)
	t4.SetStatus(state.UndoStatus)

	restart.MarkTaskAsRestartBoundary(t4, restart.RestartBoundaryDirectionUndo)

	// Requesting a reboot for task8 will put it's halt-tasks into Wait status, with their
	// WaitedStatus set to Do.
	err = restart.FinishTaskWithRestart(t4, state.UndoneStatus, restart.RestartSystem, "some-snap", nil)
	c.Assert(err, IsNil)
	c.Check(t4.Status(), Equals, state.WaitStatus)
	c.Check(t4.WaitedStatus(), Equals, state.UndoneStatus)
	c.Check(t3.Status(), Equals, state.UndoStatus)

	// A change can't be pending if the tasks that are waiting, with completion statuses
	// set to 'Undo'/'Undone' have wait-tasks which are not set to 'Undo'.
	t3.SetStatus(state.DoStatus)
	c.Check(chg2.IsReady(), Equals, false)
	c.Check(restart.PendingForChange(st, chg2), Equals, false)
}

func (s *restartSuite) TestMarkTaskForRestartWait(c *C) {
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

func (s *restartSuite) TestMarkTaskForRestartNoWait(c *C) {
	st := state.New(nil)
	st.Lock()
	defer st.Unlock()

	_, err := restart.Manager(st, "boot-id-1", nil)
	c.Assert(err, IsNil)

	chg := st.NewChange("test", "...")
	t1 := st.NewTask("foo", "...")
	chg.AddTask(t1)

	restart.MarkTaskForRestart(t1, state.DoneStatus, false)

	// No boot id set for the task
	var waitBootID string
	if err := t1.Get("wait-for-system-restart-from-boot-id", &waitBootID); !errors.Is(err, state.ErrNoState) {
		c.Check(err, IsNil)
	}
	c.Check(waitBootID, Equals, "")
	c.Check(t1.Status(), Equals, state.DoneStatus)
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

func (s *restartSuite) TestFinishTaskWithRestartDoneWithoutRestartBoundary(c *C) {
	// Having no restart-boundary acts like all restarts are restart boundaries
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

	err = restart.FinishTaskWithRestart(t, state.DoneStatus, restart.RestartSystemNow, "some-snap", nil)
	c.Assert(err, IsNil)
	c.Check(t.Status(), Equals, state.WaitStatus)
	c.Check(t.WaitedStatus(), Equals, state.DoneStatus)
	c.Check(chg.Status(), Equals, state.WaitStatus)

	// Expect the boot-id to be set here, as without any restart boundaries, all
	// requests are restart boundaries.
	var waitBootID string
	if err := t.Get("wait-for-system-restart-from-boot-id", &waitBootID); !errors.Is(err, state.ErrNoState) {
		c.Check(err, IsNil)
	}
	c.Check(waitBootID, Equals, "boot-id-1")

	c.Check(h.restartRequested, Equals, true)
	c.Check(h.restartType, Equals, restart.RestartSystemNow)
	c.Check(h.rebootInfo.BootloaderOptions, IsNil)
}

func (s *restartSuite) TestFinishTaskWithRestartDoneWithRestartBoundary(c *C) {
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

	err = restart.FinishTaskWithRestart(t, state.DoneStatus, restart.RestartSystemNow, "some-snap", nil)
	c.Assert(err, IsNil)
	c.Check(t.Status(), Equals, state.WaitStatus)
	c.Check(t.WaitedStatus(), Equals, state.DoneStatus)

	// Expect the boot-id to be set, as the task is a restart boundary
	var waitBootID string
	if err := t.Get("wait-for-system-restart-from-boot-id", &waitBootID); !errors.Is(err, state.ErrNoState) {
		c.Check(err, IsNil)
	}
	c.Check(waitBootID, Equals, "boot-id-1")

	c.Check(h.restartRequested, Equals, true)
	c.Check(h.rebootInfo, DeepEquals, &boot.RebootInfo{RebootRequired: true})
	c.Check(h.restartType, Equals, restart.RestartSystemNow)
}

func (s *restartSuite) TestFinishTaskWithRestartDoneWithRestartBoundaryAndTwoRequesters(c *C) {
	restore := release.MockOnClassic(false)
	defer restore()

	st := state.New(nil)
	st.Lock()
	defer st.Unlock()

	h := &testHandler{}
	_, err := restart.Manager(st, "boot-id-1", h)
	c.Assert(err, IsNil)

	chg := st.NewChange("test", "...")
	t1 := st.NewTask("request-and-not-wait", "...")
	t2 := st.NewTask("request-and-wait", "...")
	chg.AddTask(t1)
	chg.AddTask(t2)

	restart.MarkTaskAsRestartBoundary(t2, restart.RestartBoundaryDirectionDo)

	err = restart.FinishTaskWithRestart(t1, state.DoneStatus, restart.RestartSystem, "some-snap", nil)
	c.Assert(err, IsNil)
	c.Check(t1.Status(), Equals, state.DoneStatus)

	// Boot-id should not be set here
	var waitBootID string
	if err := t2.Get("wait-for-system-restart-from-boot-id", &waitBootID); !errors.Is(err, state.ErrNoState) {
		c.Check(err, IsNil)
	}
	c.Check(waitBootID, Equals, "")

	err = restart.FinishTaskWithRestart(t2, state.DoneStatus, restart.RestartSystemNow, "some-snap", nil)
	c.Assert(err, IsNil)
	c.Check(t2.Status(), Equals, state.WaitStatus)
	c.Check(t2.WaitedStatus(), Equals, state.DoneStatus)

	// Expect the boot-id to be set, as the task is a restart boundary
	if err := t2.Get("wait-for-system-restart-from-boot-id", &waitBootID); !errors.Is(err, state.ErrNoState) {
		c.Check(err, IsNil)
	}
	c.Check(waitBootID, Equals, "boot-id-1")

	c.Check(h.restartRequested, Equals, true)
	c.Check(h.rebootInfo, DeepEquals, &boot.RebootInfo{RebootRequired: true})
	c.Check(h.restartType, Equals, restart.RestartSystemNow)
}

func (s *restartSuite) TestFinishTaskWithRestartWithArguments(c *C) {
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

	err = restart.FinishTaskWithRestart(t, state.DoneStatus, restart.RestartSystemNow, "some-snap",
		&boot.RebootInfo{
			BootloaderOptions: &bootloader.Options{
				PrepareImageTime: true,
				Role:             bootloader.RoleRunMode,
			},
		})
	c.Assert(err, IsNil)
	c.Check(t.Status(), Equals, state.WaitStatus)
	c.Check(t.WaitedStatus(), Equals, state.DoneStatus)
	c.Check(h.restartRequested, Equals, true)
	c.Check(h.rebootInfo, DeepEquals, &boot.RebootInfo{
		RebootRequired: true,
		BootloaderOptions: &bootloader.Options{
			PrepareImageTime: true,
			Role:             bootloader.RoleRunMode,
		},
	})
	c.Check(h.restartType, Equals, restart.RestartSystemNow)
}

func (s *restartSuite) TestFinishTaskWithRestartUndoneClassic(c *C) {
	restore := release.MockOnClassic(true)
	defer restore()

	mockLog, restore := logger.MockLogger()
	defer restore()

	st := state.New(nil)
	st.Lock()
	defer st.Unlock()

	_, err := restart.Manager(st, "boot-id-1", nil)
	c.Assert(err, IsNil)

	chg := st.NewChange("test", "...")
	t := st.NewTask("waiting", "...")

	chg.AddTask(t)

	// Ensure a restart is not requested when undoing on classic
	err = restart.FinishTaskWithRestart(t, state.UndoneStatus, restart.RestartSystemNow, "some-snap", nil)
	c.Assert(err, IsNil)
	c.Check(t.Status(), Equals, state.UndoneStatus)

	c.Check(t.Log(), HasLen, 1)
	c.Check(t.Log()[0], Matches, ".* Skipped automatic system restart on classic system when undoing changes back to previous state")
	c.Check(mockLog.String(), Not(Matches), ".* Postponing restart until a manual system restart allows to continue")
}

func (s *restartSuite) TestFinishTaskWithRestartUndoneCore(c *C) {
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

	err = restart.FinishTaskWithRestart(t, state.UndoneStatus, restart.RestartSystemNow, "some-snap", nil)
	c.Assert(err, IsNil)
	c.Check(t.Status(), Equals, state.WaitStatus)
	c.Check(t.WaitedStatus(), Equals, state.UndoneStatus)
	c.Check(chg.Status(), Equals, state.WaitStatus)

	// Restart must still be requested
	c.Check(h.restartRequested, Equals, true)
	c.Check(h.rebootInfo, DeepEquals, &boot.RebootInfo{RebootRequired: true})
	c.Check(h.restartType, Equals, restart.RestartSystemNow)
}

func (s *restartSuite) TestFinishTaskWithRestart2UndoneCoreWithRestartBoundary(c *C) {
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

	restart.MarkTaskAsRestartBoundary(t, restart.RestartBoundaryDirectionUndo)

	err = restart.FinishTaskWithRestart(t, state.UndoneStatus, restart.RestartSystemNow, "some-snap", nil)
	c.Assert(err, IsNil)
	c.Check(t.Status(), Equals, state.WaitStatus)
	c.Check(t.WaitedStatus(), Equals, state.UndoneStatus)
	c.Check(h.restartRequested, Equals, true)
	c.Check(h.rebootInfo, DeepEquals, &boot.RebootInfo{RebootRequired: true})
	c.Check(h.restartType, Equals, restart.RestartSystemNow)
}

func (s *restartSuite) TestFinishTaskWithRestartInvalid(c *C) {
	st := state.New(nil)
	st.Lock()
	defer st.Unlock()

	chg := st.NewChange("test", "...")
	t := st.NewTask("waiting", "...")

	chg.AddTask(t)

	err := restart.FinishTaskWithRestart(t, state.DoingStatus, restart.RestartSystem, "", nil)
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
	chg         *state.Change
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
	s.chg = s.st.NewChange("not-ready", "...")
	s.t1 = s.st.NewTask("task", "...")
	restart.MarkTaskAsRestartBoundary(s.t1, restart.RestartBoundaryDirectionDo)
	s.chg.AddTask(s.t1)
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
	c.Check(err, IsNil)

	c.Check(mockNrr.Calls(), DeepEquals, [][]string{
		{"notify-reboot-required", "snap:some-snap"},
	})
	c.Check(s.mockLog.String(), Matches, ".* Postponing restart until a manual system restart allows to continue\n")
}

func (s *notifyRebootRequiredSuite) TestFinishTaskWithRestartNotifiesRebootRequiredLogsErr(c *C) {
	s.st.Lock()
	defer s.st.Unlock()

	mockNrr := testutil.MockCommand(c, s.mockNrrPath, `echo fail; exit 1`)
	defer mockNrr.Restore()

	err := restart.FinishTaskWithRestart(s.t1, state.DoneStatus, restart.RestartSystem, "some-snap", nil)
	c.Check(err, IsNil)
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
