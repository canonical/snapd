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

package hookstate_test

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"regexp"
	"sync/atomic"
	"testing"
	"time"

	. "gopkg.in/check.v1"
	"gopkg.in/tomb.v2"

	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/overlord"
	"github.com/snapcore/snapd/overlord/hookstate"
	"github.com/snapcore/snapd/overlord/hookstate/hooktest"
	"github.com/snapcore/snapd/overlord/restart"
	"github.com/snapcore/snapd/overlord/snapstate"
	"github.com/snapcore/snapd/overlord/snapstate/sequence"
	"github.com/snapcore/snapd/overlord/snapstate/snapstatetest"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/snap/naming"
	"github.com/snapcore/snapd/snap/snaptest"
	"github.com/snapcore/snapd/testutil"
)

func TestHookManager(t *testing.T) { TestingT(t) }

type baseHookManagerSuite struct {
	testutil.BaseTest

	o           *overlord.Overlord
	state       *state.State
	se          *overlord.StateEngine
	manager     *hookstate.HookManager
	context     *hookstate.Context
	mockHandler *hooktest.MockHandler
	task        *state.Task
	change      *state.Change
	command     *testutil.MockCmd
}

var (
	settleTimeout = testutil.HostScaledTimeout(15 * time.Second)
)

func (s *baseHookManagerSuite) commonSetUpTest(c *C) {
	s.BaseTest.SetUpTest(c)

	hooktype1 := snap.NewHookType(regexp.MustCompile("^do-something$"))
	hooktype2 := snap.NewHookType(regexp.MustCompile("^undo-something$"))
	s.AddCleanup(snap.MockAppendSupportedHookTypes([]*snap.HookType{hooktype1, hooktype2}))

	dirs.SetRootDir(c.MkDir())
	s.o = overlord.Mock()
	s.state = s.o.State()
	s.state.Lock()
	_, err := restart.Manager(s.state, "boot-id-0", nil)
	s.state.Unlock()
	c.Assert(err, IsNil)
	manager, err := hookstate.Manager(s.state, s.o.TaskRunner())
	c.Assert(err, IsNil)
	s.manager = manager
	s.se = s.o.StateEngine()
	s.o.AddManager(s.manager)
	s.o.AddManager(s.o.TaskRunner())
	c.Assert(s.o.StartUp(), IsNil)

	s.AddCleanup(snap.MockSanitizePlugsSlots(func(snapInfo *snap.Info) {}))

	s.command = testutil.MockCommand(c, "snap", "")
	s.AddCleanup(s.command.Restore)

	s.context = nil
	s.mockHandler = hooktest.NewMockHandler()
	s.manager.Register(regexp.MustCompile("configure"), func(context *hookstate.Context) hookstate.Handler {
		s.context = context
		return s.mockHandler
	})
}

func (s *baseHookManagerSuite) commonTearDownTest(c *C) {
	s.BaseTest.TearDownTest(c)

	s.manager.StopHooks()
	s.se.Stop()
	dirs.SetRootDir("")
}

func (s *baseHookManagerSuite) setUpSnap(c *C, instanceName string, yaml string) {
	hooksup := &hookstate.HookSetup{
		Snap:     instanceName,
		Hook:     "configure",
		Revision: snap.R(1),
	}

	initialContext := map[string]interface{}{
		"test-key": "test-value",
	}

	s.state.Lock()
	defer s.state.Unlock()
	s.task = hookstate.HookTask(s.state, "test summary", hooksup, initialContext)
	c.Assert(s.task, NotNil, Commentf("Expected HookTask to return a task"))

	s.change = s.state.NewChange("kind", "summary")
	s.change.AddTask(s.task)

	snapName, instanceKey := snap.SplitInstanceName(instanceName)

	sideInfo := &snap.SideInfo{RealName: snapName, SnapID: "some-snap-id", Revision: snap.R(1)}
	snaptest.MockSnapInstance(c, instanceName, yaml, sideInfo)
	snapstate.Set(s.state, instanceName, &snapstate.SnapState{
		Active:      true,
		Sequence:    snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{sideInfo}),
		Current:     snap.R(1),
		InstanceKey: instanceKey,
	})
}

type hookManagerSuite struct {
	baseHookManagerSuite
}

var _ = Suite(&hookManagerSuite{})

var snapYaml = `
name: test-snap
version: 1.0
hooks:
    configure:
    prepare-device:
    do-something:
    undo-something:
`

var snapYaml1 = `
name: test-snap-1
version: 1.0
hooks:
    prepare-device:
`

var snapYaml2 = `
name: test-snap-2
version: 1.0
hooks:
    prepare-device:
`

func (s *hookManagerSuite) SetUpTest(c *C) {
	s.commonSetUpTest(c)

	s.setUpSnap(c, "test-snap", snapYaml)
}

func (s *hookManagerSuite) TearDownTest(c *C) {
	s.commonTearDownTest(c)
}

func (s *hookManagerSuite) settle(c *C) {
	err := s.o.Settle(settleTimeout)
	c.Assert(err, IsNil)
}

func (s *hookManagerSuite) TestSmoke(c *C) {
	s.se.Ensure()
	s.se.Wait()
}

func (s *hookManagerSuite) TestHookSetupJsonMarshal(c *C) {
	hookSetup := &hookstate.HookSetup{Snap: "snap-name", Revision: snap.R(1), Hook: "hook-name"}
	out, err := json.Marshal(hookSetup)
	c.Assert(err, IsNil)
	c.Check(string(out), Equals, "{\"snap\":\"snap-name\",\"revision\":\"1\",\"hook\":\"hook-name\",\"component-revision\":\"unset\"}")
}

func (s *hookManagerSuite) TestHookSetupJsonUnmarshal(c *C) {
	out, err := json.Marshal(hookstate.HookSetup{Snap: "snap-name", Revision: snap.R(1), Hook: "hook-name"})
	c.Assert(err, IsNil)

	var setup hookstate.HookSetup
	err = json.Unmarshal(out, &setup)
	c.Assert(err, IsNil)
	c.Check(setup.Snap, Equals, "snap-name")
	c.Check(setup.Revision, Equals, snap.R(1))
	c.Check(setup.Hook, Equals, "hook-name")
}

func (s *hookManagerSuite) TestHookTask(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	hooksup := &hookstate.HookSetup{
		Snap:     "test-snap",
		Hook:     "configure",
		Revision: snap.R(1),
	}

	task := hookstate.HookTask(s.state, "test summary", hooksup, nil)
	c.Check(task.Kind(), Equals, "run-hook")

	var setup hookstate.HookSetup
	err := task.Get("hook-setup", &setup)
	c.Check(err, IsNil)
	c.Check(setup.Snap, Equals, "test-snap")
	c.Check(setup.Revision, Equals, snap.R(1))
	c.Check(setup.Hook, Equals, "configure")
}

func (s *hookManagerSuite) TestHookTaskEnsure(c *C) {
	didRun := make(chan bool)
	s.mockHandler.BeforeCallback = func() {
		c.Check(s.manager.NumRunningHooks(), Equals, 1)
		go func() {
			didRun <- s.manager.GracefullyWaitRunningHooks()
		}()
	}
	s.se.Ensure()
	select {
	case ok := <-didRun:
		c.Check(ok, Equals, true)
	case <-time.After(5 * time.Second):
		c.Fatal("hook run should have been done by now")
	}
	s.se.Wait()

	s.state.Lock()
	defer s.state.Unlock()

	c.Assert(s.context, NotNil, Commentf("Expected handler generator to be called with a valid context"))
	c.Check(s.context.InstanceName(), Equals, "test-snap")
	c.Check(s.context.SnapRevision(), Equals, snap.R(1))
	c.Check(s.context.HookName(), Equals, "configure")

	c.Check(s.command.Calls(), DeepEquals, [][]string{{
		"snap", "run", "--hook", "configure", "-r", "1", "test-snap",
	}})

	c.Check(s.mockHandler.BeforeCalled, Equals, true)
	c.Check(s.mockHandler.DoneCalled, Equals, true)
	c.Check(s.mockHandler.ErrorCalled, Equals, false)

	c.Check(s.task.Kind(), Equals, "run-hook")
	c.Check(s.task.Status(), Equals, state.DoneStatus)
	c.Check(s.change.Status(), Equals, state.DoneStatus)

	c.Check(s.manager.NumRunningHooks(), Equals, 0)
}

func (s *hookManagerSuite) TestHookTaskEnsureRestarting(c *C) {
	// we do no start new hooks runs if we are restarting
	s.state.Lock()
	restart.MockPending(s.state, restart.RestartDaemon)
	s.state.Unlock()

	s.se.Ensure()
	s.se.Wait()

	s.state.Lock()
	defer s.state.Unlock()

	c.Assert(s.context, IsNil)

	c.Check(s.command.Calls(), HasLen, 0)

	c.Check(s.mockHandler.BeforeCalled, Equals, false)
	c.Check(s.mockHandler.DoneCalled, Equals, false)
	c.Check(s.mockHandler.ErrorCalled, Equals, false)

	c.Check(s.task.Status(), Equals, state.DoingStatus)
	c.Check(s.change.Status(), Equals, state.DoingStatus)

	c.Check(s.manager.NumRunningHooks(), Equals, 0)
}

func (s *hookManagerSuite) TestHookSnapMissing(c *C) {
	s.state.Lock()
	snapstate.Set(s.state, "test-snap", nil)
	s.state.Unlock()

	s.se.Ensure()
	s.se.Wait()

	s.state.Lock()
	defer s.state.Unlock()

	c.Check(s.change.Err(), ErrorMatches, `(?s).*cannot find "test-snap" snap.*`)
}

func (s *hookManagerSuite) TestHookHijackingHappy(c *C) {
	// this works even if test-snap is not present
	s.state.Lock()
	snapstate.Set(s.state, "test-snap", nil)
	s.state.Unlock()

	var hijackedContext *hookstate.Context
	s.manager.RegisterHijack("configure", "test-snap", func(ctx *hookstate.Context) error {
		hijackedContext = ctx
		return nil
	})

	s.se.Ensure()
	s.se.Wait()

	s.state.Lock()
	defer s.state.Unlock()

	c.Check(hijackedContext, DeepEquals, s.context)
	c.Check(s.command.Calls(), HasLen, 0)

	c.Assert(s.context, NotNil)
	c.Check(s.context.InstanceName(), Equals, "test-snap")
	c.Check(s.context.SnapRevision(), Equals, snap.R(1))
	c.Check(s.context.HookName(), Equals, "configure")

	c.Check(s.mockHandler.BeforeCalled, Equals, true)
	c.Check(s.mockHandler.DoneCalled, Equals, true)
	c.Check(s.mockHandler.ErrorCalled, Equals, false)

	c.Check(s.task.Kind(), Equals, "run-hook")
	c.Check(s.task.Status(), Equals, state.DoneStatus)
	c.Check(s.change.Status(), Equals, state.DoneStatus)
}

func (s *hookManagerSuite) TestHookHijackingUnHappy(c *C) {
	s.manager.RegisterHijack("configure", "test-snap", func(ctx *hookstate.Context) error {
		return fmt.Errorf("not-happy-at-all")
	})

	s.se.Ensure()
	s.se.Wait()

	s.state.Lock()
	defer s.state.Unlock()

	c.Check(s.command.Calls(), HasLen, 0)

	c.Assert(s.context, NotNil)
	c.Check(s.context.InstanceName(), Equals, "test-snap")
	c.Check(s.context.SnapRevision(), Equals, snap.R(1))
	c.Check(s.context.HookName(), Equals, "configure")

	c.Check(s.mockHandler.BeforeCalled, Equals, true)
	c.Check(s.mockHandler.DoneCalled, Equals, false)
	c.Check(s.mockHandler.ErrorCalled, Equals, true)

	c.Check(s.task.Kind(), Equals, "run-hook")
	c.Check(s.task.Status(), Equals, state.ErrorStatus)
	c.Check(s.change.Status(), Equals, state.ErrorStatus)
}

func (s *hookManagerSuite) TestHookHijackingVeryUnHappy(c *C) {
	f := func(ctx *hookstate.Context) error {
		return nil
	}
	s.manager.RegisterHijack("configure", "test-snap", f)
	c.Check(func() { s.manager.RegisterHijack("configure", "test-snap", f) }, PanicMatches, "hook configure for snap test-snap already hijacked")
}

func (s *hookManagerSuite) TestHookTaskInitializesContext(c *C) {
	s.se.Ensure()
	s.se.Wait()

	var value string
	c.Assert(s.context, NotNil, Commentf("Expected handler generator to be called with a valid context"))
	s.context.Lock()
	defer s.context.Unlock()
	c.Check(s.context.Get("test-key", &value), IsNil, Commentf("Expected context to be initialized"))
	c.Check(value, Equals, "test-value")
}

func (s *hookManagerSuite) TestHookTaskHandlesHookError(c *C) {
	// Force the snap command to exit 1, and print something to stderr
	cmd := testutil.MockCommand(
		c, "snap", ">&2 echo 'hook failed at user request'; exit 1")
	defer cmd.Restore()

	s.se.Ensure()
	s.se.Wait()

	s.state.Lock()
	defer s.state.Unlock()

	c.Check(s.mockHandler.BeforeCalled, Equals, true)
	c.Check(s.mockHandler.DoneCalled, Equals, false)
	c.Check(s.mockHandler.ErrorCalled, Equals, true)

	c.Check(s.task.Kind(), Equals, "run-hook")
	c.Check(s.task.Status(), Equals, state.ErrorStatus)
	c.Check(s.change.Status(), Equals, state.ErrorStatus)
	checkTaskLogContains(c, s.task, ".*failed at user request.*")

	c.Check(s.manager.NumRunningHooks(), Equals, 0)
}

func (s *hookManagerSuite) TestHookTaskHandleIgnoreErrorWorks(c *C) {
	s.state.Lock()
	var hooksup hookstate.HookSetup
	s.task.Get("hook-setup", &hooksup)
	hooksup.IgnoreError = true
	s.task.Set("hook-setup", &hooksup)
	s.state.Unlock()

	// Force the snap command to exit 1, and print something to stderr
	cmd := testutil.MockCommand(
		c, "snap", ">&2 echo 'hook failed at user request'; exit 1")
	defer cmd.Restore()

	s.se.Ensure()
	s.se.Wait()

	s.state.Lock()
	defer s.state.Unlock()

	c.Check(s.mockHandler.BeforeCalled, Equals, true)
	c.Check(s.mockHandler.DoneCalled, Equals, true)
	c.Check(s.mockHandler.ErrorCalled, Equals, false)

	c.Check(s.task.Kind(), Equals, "run-hook")
	c.Check(s.task.Status(), Equals, state.DoneStatus)
	c.Check(s.change.Status(), Equals, state.DoneStatus)
	checkTaskLogContains(c, s.task, ".*ignoring failure in hook.*")
}

func (s *hookManagerSuite) TestHookTaskHandlesHookErrorAndIgnoresIt(c *C) {
	// tell the mock handler to return 'true' from its Error() handler,
	// indicating to the hookmgr to ignore the original hook error.
	s.mockHandler.IgnoreOriginalErr = true

	// Simulate hook error
	cmd := testutil.MockCommand(
		c, "snap", ">&2 echo 'hook failed at user request'; exit 1")
	defer cmd.Restore()

	s.se.Ensure()
	s.se.Wait()

	s.state.Lock()
	defer s.state.Unlock()

	c.Check(s.mockHandler.BeforeCalled, Equals, true)
	c.Check(s.mockHandler.DoneCalled, Equals, false)
	c.Check(s.mockHandler.ErrorCalled, Equals, true)

	c.Check(s.task.Kind(), Equals, "run-hook")
	c.Check(s.task.Status(), Equals, state.DoneStatus)
	c.Check(s.change.Status(), Equals, state.DoneStatus)

	c.Check(s.manager.NumRunningHooks(), Equals, 0)
}

func (s *hookManagerSuite) TestHookTaskEnforcesTimeout(c *C) {
	var hooksup hookstate.HookSetup

	s.state.Lock()
	s.task.Get("hook-setup", &hooksup)
	hooksup.Timeout = time.Duration(200 * time.Millisecond)
	s.task.Set("hook-setup", &hooksup)
	s.state.Unlock()

	// Force the snap command to hang
	cmd := testutil.MockCommand(c, "snap", "while true; do sleep 1; done")
	defer cmd.Restore()

	s.se.Ensure()
	s.se.Wait()

	s.state.Lock()
	defer s.state.Unlock()

	c.Check(s.mockHandler.BeforeCalled, Equals, true)
	c.Check(s.mockHandler.DoneCalled, Equals, false)
	c.Check(s.mockHandler.ErrorCalled, Equals, true)
	c.Check(s.mockHandler.Err, ErrorMatches, `.*exceeded maximum runtime of 200ms.*`)

	c.Check(s.task.Kind(), Equals, "run-hook")
	c.Check(s.task.Status(), Equals, state.ErrorStatus)
	c.Check(s.change.Status(), Equals, state.ErrorStatus)
	checkTaskLogContains(c, s.task, `.*exceeded maximum runtime of 200ms`)
}

func (s *hookManagerSuite) TestHookTaskEnforcesDefaultTimeout(c *C) {
	restore := hookstate.MockDefaultHookTimeout(150 * time.Millisecond)
	defer restore()

	// Force the snap command to hang
	cmd := testutil.MockCommand(c, "snap", "while true; do sleep 1; done")
	defer cmd.Restore()

	s.se.Ensure()
	s.se.Wait()

	s.state.Lock()
	defer s.state.Unlock()

	c.Check(s.mockHandler.BeforeCalled, Equals, true)
	c.Check(s.mockHandler.DoneCalled, Equals, false)
	c.Check(s.mockHandler.ErrorCalled, Equals, true)
	c.Check(s.mockHandler.Err, ErrorMatches, `.*exceeded maximum runtime of 150ms.*`)

	c.Check(s.task.Kind(), Equals, "run-hook")
	c.Check(s.task.Status(), Equals, state.ErrorStatus)
	c.Check(s.change.Status(), Equals, state.ErrorStatus)
	checkTaskLogContains(c, s.task, `.*exceeded maximum runtime of 150ms`)
}

func (s *hookManagerSuite) TestHookTaskEnforcedTimeoutWithIgnoreError(c *C) {
	var hooksup hookstate.HookSetup

	s.state.Lock()
	s.task.Get("hook-setup", &hooksup)
	hooksup.Timeout = time.Duration(200 * time.Millisecond)
	hooksup.IgnoreError = true
	s.task.Set("hook-setup", &hooksup)
	s.state.Unlock()

	// Force the snap command to hang
	cmd := testutil.MockCommand(c, "snap", "while true; do sleep 1; done")
	defer cmd.Restore()

	s.se.Ensure()
	s.se.Wait()

	s.state.Lock()
	defer s.state.Unlock()

	c.Check(s.mockHandler.BeforeCalled, Equals, true)
	c.Check(s.mockHandler.DoneCalled, Equals, true)
	c.Check(s.mockHandler.ErrorCalled, Equals, false)
	c.Check(s.mockHandler.Err, IsNil)

	c.Check(s.task.Kind(), Equals, "run-hook")
	c.Check(s.task.Status(), Equals, state.DoneStatus)
	c.Check(s.change.Status(), Equals, state.DoneStatus)
	checkTaskLogContains(c, s.task, `.*ignoring failure in hook.*exceeded maximum runtime of 200ms`)
}

func (s *hookManagerSuite) TestHookTaskCanKillHook(c *C) {
	// Force the snap command to hang
	cmd := testutil.MockCommand(c, "snap", "while true; do sleep 1; done")
	defer cmd.Restore()

	s.se.Ensure()

	// Abort the change, which should kill the hanging hook, and
	// wait for the task to complete.
	s.state.Lock()
	s.change.Abort()
	s.state.Unlock()
	s.se.Ensure()
	s.se.Wait()

	s.state.Lock()
	defer s.state.Unlock()

	c.Check(s.mockHandler.BeforeCalled, Equals, true)
	c.Check(s.mockHandler.DoneCalled, Equals, false)
	c.Check(s.mockHandler.ErrorCalled, Equals, true)
	c.Check(s.mockHandler.Err, ErrorMatches, "<aborted>")

	c.Check(s.task.Kind(), Equals, "run-hook")
	c.Check(s.task.Status(), Equals, state.ErrorStatus)
	c.Check(s.change.Status(), Equals, state.ErrorStatus)
	checkTaskLogContains(c, s.task, `run hook "[^"]*": <aborted>`)

	c.Check(s.manager.NumRunningHooks(), Equals, 0)
}

func (s *hookManagerSuite) TestHookTaskCorrectlyIncludesContext(c *C) {
	// Force the snap command to exit with a failure and print to stderr so we
	// can catch and verify it.
	cmd := testutil.MockCommand(
		c, "snap", ">&2 echo \"SNAP_COOKIE=$SNAP_COOKIE\"; exit 1")
	defer cmd.Restore()

	s.se.Ensure()
	s.se.Wait()

	s.state.Lock()
	defer s.state.Unlock()

	c.Check(s.mockHandler.BeforeCalled, Equals, true)
	c.Check(s.mockHandler.DoneCalled, Equals, false)
	c.Check(s.mockHandler.ErrorCalled, Equals, true)

	c.Check(s.task.Kind(), Equals, "run-hook")
	c.Check(s.task.Status(), Equals, state.ErrorStatus)
	c.Check(s.change.Status(), Equals, state.ErrorStatus)
	checkTaskLogContains(c, s.task, `.*SNAP_COOKIE=\S+`)
}

func (s *hookManagerSuite) TestHookTaskHandlerBeforeError(c *C) {
	s.mockHandler.BeforeError = true

	s.se.Ensure()
	s.se.Wait()

	s.state.Lock()
	defer s.state.Unlock()

	c.Check(s.mockHandler.BeforeCalled, Equals, true)
	c.Check(s.mockHandler.DoneCalled, Equals, false)
	c.Check(s.mockHandler.ErrorCalled, Equals, false)

	c.Check(s.task.Kind(), Equals, "run-hook")
	c.Check(s.task.Status(), Equals, state.ErrorStatus)
	c.Check(s.change.Status(), Equals, state.ErrorStatus)
	checkTaskLogContains(c, s.task, `.*Before failed at user request.*`)
}

func (s *hookManagerSuite) TestHookTaskHandlerDoneError(c *C) {
	s.mockHandler.DoneError = true

	s.se.Ensure()
	s.se.Wait()

	s.state.Lock()
	defer s.state.Unlock()

	c.Check(s.mockHandler.BeforeCalled, Equals, true)
	c.Check(s.mockHandler.DoneCalled, Equals, true)
	c.Check(s.mockHandler.ErrorCalled, Equals, false)

	c.Check(s.task.Kind(), Equals, "run-hook")
	c.Check(s.task.Status(), Equals, state.ErrorStatus)
	c.Check(s.change.Status(), Equals, state.ErrorStatus)
	checkTaskLogContains(c, s.task, `.*Done failed at user request.*`)
}

func (s *hookManagerSuite) TestHookTaskHandlerErrorError(c *C) {
	s.mockHandler.ErrorError = true

	// Force the snap command to simply exit 1, so the handler Error() runs
	cmd := testutil.MockCommand(c, "snap", "exit 1")
	defer cmd.Restore()

	s.se.Ensure()
	s.se.Wait()

	s.state.Lock()
	defer s.state.Unlock()

	c.Check(s.mockHandler.BeforeCalled, Equals, true)
	c.Check(s.mockHandler.DoneCalled, Equals, false)
	c.Check(s.mockHandler.ErrorCalled, Equals, true)

	c.Check(s.task.Kind(), Equals, "run-hook")
	c.Check(s.task.Status(), Equals, state.ErrorStatus)
	c.Check(s.change.Status(), Equals, state.ErrorStatus)
	checkTaskLogContains(c, s.task, `.*Error failed at user request.*`)
}

func (s *hookManagerSuite) TestHookUndoRunsOnError(c *C) {
	handler := hooktest.NewMockHandler()
	undoHandler := hooktest.NewMockHandler()

	s.manager.Register(regexp.MustCompile("^do-something$"), func(context *hookstate.Context) hookstate.Handler {
		return handler
	})
	s.manager.Register(regexp.MustCompile("^undo-something$"), func(context *hookstate.Context) hookstate.Handler {
		return undoHandler
	})

	hooksup := &hookstate.HookSetup{
		Snap:     "test-snap",
		Hook:     "do-something",
		Revision: snap.R(1),
	}
	undohooksup := &hookstate.HookSetup{
		Snap:     "test-snap",
		Hook:     "undo-something",
		Revision: snap.R(1),
	}

	// use unknown hook to fail the change
	failinghooksup := &hookstate.HookSetup{
		Snap:     "test-snap",
		Hook:     "unknown-hook",
		Revision: snap.R(1),
	}

	initialContext := map[string]interface{}{}

	s.state.Lock()
	task := hookstate.HookTaskWithUndo(s.state, "test summary", hooksup, undohooksup, initialContext)
	c.Assert(task, NotNil)
	failtask := hookstate.HookTask(s.state, "test summary", failinghooksup, initialContext)
	failtask.WaitFor(task)

	change := s.state.NewChange("kind", "summary")
	change.AddTask(task)
	change.AddTask(failtask)
	s.state.Unlock()

	s.settle(c)

	s.state.Lock()
	defer s.state.Unlock()

	c.Check(handler.BeforeCalled, Equals, true)
	c.Check(handler.DoneCalled, Equals, true)
	c.Check(handler.ErrorCalled, Equals, false)

	c.Check(undoHandler.BeforeCalled, Equals, true)
	c.Check(undoHandler.DoneCalled, Equals, true)
	c.Check(undoHandler.ErrorCalled, Equals, false)

	c.Check(task.Status(), Equals, state.UndoneStatus)
	c.Check(change.Status(), Equals, state.ErrorStatus)

	c.Check(s.manager.NumRunningHooks(), Equals, 0)
}

func (s *hookManagerSuite) TestHookWithoutHandlerIsError(c *C) {
	hooksup := &hookstate.HookSetup{
		Snap:     "test-snap",
		Hook:     "prepare-device",
		Revision: snap.R(1),
	}
	s.state.Lock()
	s.task.Set("hook-setup", hooksup)
	s.state.Unlock()

	s.se.Ensure()
	s.se.Wait()

	s.state.Lock()
	defer s.state.Unlock()

	c.Check(s.task.Kind(), Equals, "run-hook")
	c.Check(s.task.Status(), Equals, state.ErrorStatus)
	c.Check(s.change.Status(), Equals, state.ErrorStatus)
	checkTaskLogContains(c, s.task, `.*no registered handlers for hook "prepare-device".*`)
}

func (s *hookManagerSuite) TestHookWithMultipleHandlersIsError(c *C) {
	// Register multiple times for this hook
	s.manager.Register(regexp.MustCompile("configure"), func(context *hookstate.Context) hookstate.Handler {
		return hooktest.NewMockHandler()
	})

	s.se.Ensure()
	s.se.Wait()

	s.state.Lock()
	defer s.state.Unlock()

	c.Check(s.task.Kind(), Equals, "run-hook")
	c.Check(s.task.Status(), Equals, state.ErrorStatus)
	c.Check(s.change.Status(), Equals, state.ErrorStatus)

	checkTaskLogContains(c, s.task, `.*2 handlers registered for hook "configure".*`)
}

func (s *hookManagerSuite) TestHookWithoutHookIsError(c *C) {
	hooksup := &hookstate.HookSetup{
		Snap: "test-snap",
		Hook: "missing-hook",
	}
	s.state.Lock()
	s.task.Set("hook-setup", hooksup)
	s.state.Unlock()

	s.se.Ensure()
	s.se.Wait()

	s.state.Lock()
	defer s.state.Unlock()

	c.Check(s.task.Kind(), Equals, "run-hook")
	c.Check(s.task.Status(), Equals, state.ErrorStatus)
	c.Check(s.change.Status(), Equals, state.ErrorStatus)
	checkTaskLogContains(c, s.task, `.*snap "test-snap" has no "missing-hook" hook`)
}

func (s *hookManagerSuite) TestHookWithoutHookOptional(c *C) {
	s.manager.Register(regexp.MustCompile("missing-hook"), func(context *hookstate.Context) hookstate.Handler {
		return s.mockHandler
	})

	hooksup := &hookstate.HookSetup{
		Snap:     "test-snap",
		Hook:     "missing-hook",
		Optional: true,
	}
	s.state.Lock()
	s.task.Set("hook-setup", hooksup)
	s.state.Unlock()

	s.se.Ensure()
	s.se.Wait()

	c.Check(s.mockHandler.BeforeCalled, Equals, false)
	c.Check(s.mockHandler.DoneCalled, Equals, false)
	c.Check(s.mockHandler.ErrorCalled, Equals, false)

	c.Check(s.command.Calls(), IsNil)

	s.state.Lock()
	defer s.state.Unlock()

	c.Check(s.task.Kind(), Equals, "run-hook")
	c.Check(s.task.Status(), Equals, state.DoneStatus)
	c.Check(s.change.Status(), Equals, state.DoneStatus)

	c.Logf("Task log:\n%s\n", s.task.Log())
}

func (s *hookManagerSuite) TestHookWithoutHookAlways(c *C) {
	s.manager.Register(regexp.MustCompile("missing-hook"), func(context *hookstate.Context) hookstate.Handler {
		return s.mockHandler
	})

	hooksup := &hookstate.HookSetup{
		Snap:     "test-snap",
		Hook:     "missing-hook",
		Optional: true,
		Always:   true,
	}
	s.state.Lock()
	s.task.Set("hook-setup", hooksup)
	s.state.Unlock()

	s.se.Ensure()
	s.se.Wait()

	c.Check(s.mockHandler.BeforeCalled, Equals, true)
	c.Check(s.mockHandler.DoneCalled, Equals, true)
	c.Check(s.mockHandler.ErrorCalled, Equals, false)

	c.Check(s.command.Calls(), IsNil)

	s.state.Lock()
	defer s.state.Unlock()

	c.Check(s.task.Kind(), Equals, "run-hook")
	c.Check(s.task.Status(), Equals, state.DoneStatus)
	c.Check(s.change.Status(), Equals, state.DoneStatus)

	c.Logf("Task log:\n%s\n", s.task.Log())
}

func (s *hookManagerSuite) TestOptionalHookWithMissingHandler(c *C) {
	hooksup := &hookstate.HookSetup{
		Snap:     "test-snap",
		Hook:     "missing-hook-and-no-handler",
		Optional: true,
	}
	s.state.Lock()
	s.task.Set("hook-setup", hooksup)
	s.state.Unlock()

	s.se.Ensure()
	s.se.Wait()

	c.Check(s.command.Calls(), IsNil)

	s.state.Lock()
	defer s.state.Unlock()

	c.Check(s.task.Kind(), Equals, "run-hook")
	c.Check(s.task.Status(), Equals, state.DoneStatus)
	c.Check(s.change.Status(), Equals, state.DoneStatus)

	c.Logf("Task log:\n%s\n", s.task.Log())
}

func checkTaskLogContains(c *C, task *state.Task, pattern string) {
	exp := regexp.MustCompile(pattern)
	found := false
	for _, message := range task.Log() {
		if exp.MatchString(message) {
			found = true
		}
	}

	c.Check(found, Equals, true, Commentf("Expected to find regex %q in task log: %v", pattern, task.Log()))
}

func (s *hookManagerSuite) TestHookTaskRunsRightSnapCmd(c *C) {
	coreSnapCmdPath := filepath.Join(dirs.SnapMountDir, "core/12/usr/bin/snap")
	cmd := testutil.MockCommand(c, coreSnapCmdPath, "")
	defer cmd.Restore()

	r := hookstate.MockReadlink(func(p string) (string, error) {
		c.Assert(p, Equals, "/proc/self/exe")
		return filepath.Join(dirs.SnapMountDir, "core/12/usr/lib/snapd/snapd"), nil
	})
	defer r()

	s.se.Ensure()
	s.se.Wait()

	s.state.Lock()
	defer s.state.Unlock()

	c.Assert(s.context, NotNil, Commentf("Expected handler generator to be called with a valid context"))
	c.Check(cmd.Calls(), DeepEquals, [][]string{{
		"snap", "run", "--hook", "configure", "-r", "1", "test-snap",
	}})

}

func (s *hookManagerSuite) TestHookTasksForSameSnapAreSerialized(c *C) {
	var Executing int32
	var TotalExecutions int32

	s.mockHandler.BeforeCallback = func() {
		executing := atomic.AddInt32(&Executing, 1)
		if executing != 1 {
			panic(fmt.Sprintf("More than one handler executed: %d", executing))
		}
	}

	s.mockHandler.DoneCallback = func() {
		executing := atomic.AddInt32(&Executing, -1)
		if executing != 0 {
			panic(fmt.Sprintf("More than one handler executed: %d", executing))
		}
		atomic.AddInt32(&TotalExecutions, 1)
	}

	hooksup := &hookstate.HookSetup{
		Snap:     "test-snap",
		Hook:     "configure",
		Revision: snap.R(1),
	}

	s.state.Lock()

	var tasks []*state.Task
	for i := 0; i < 20; i++ {
		task := hookstate.HookTask(s.state, "test summary", hooksup, nil)
		c.Assert(s.task, NotNil)
		change := s.state.NewChange("kind", "summary")
		change.AddTask(task)
		tasks = append(tasks, task)
	}
	s.state.Unlock()

	s.settle(c)

	s.state.Lock()
	defer s.state.Unlock()

	c.Check(s.task.Kind(), Equals, "run-hook")
	c.Check(s.task.Status(), Equals, state.DoneStatus)
	c.Check(s.change.Status(), Equals, state.DoneStatus)

	for i := 0; i < len(tasks); i++ {
		c.Check(tasks[i].Kind(), Equals, "run-hook")
		c.Check(tasks[i].Status(), Equals, state.DoneStatus)
	}
	c.Assert(atomic.LoadInt32(&TotalExecutions), Equals, int32(1+len(tasks)))
	c.Assert(atomic.LoadInt32(&Executing), Equals, int32(0))
}

type MockConcurrentHandler struct {
	onDone func()
}

func (h *MockConcurrentHandler) Before() error {
	return nil
}

func (h *MockConcurrentHandler) Done() error {
	h.onDone()
	return nil
}

func (h *MockConcurrentHandler) Error(err error) (bool, error) {
	return false, nil
}

func NewMockConcurrentHandler(onDone func()) *MockConcurrentHandler {
	return &MockConcurrentHandler{onDone: onDone}
}

func (s *hookManagerSuite) TestHookTasksForDifferentSnapsRunConcurrently(c *C) {
	hooksup1 := &hookstate.HookSetup{
		Snap:     "test-snap-1",
		Hook:     "prepare-device",
		Revision: snap.R(1),
	}
	hooksup2 := &hookstate.HookSetup{
		Snap:     "test-snap-2",
		Hook:     "prepare-device",
		Revision: snap.R(1),
	}

	s.state.Lock()

	sideInfo := &snap.SideInfo{RealName: "test-snap-1", SnapID: "some-snap-id1", Revision: snap.R(1)}
	info := snaptest.MockSnap(c, snapYaml1, sideInfo)
	c.Assert(info.Hooks, HasLen, 1)
	snapstate.Set(s.state, "test-snap-1", &snapstate.SnapState{
		Active:   true,
		Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{sideInfo}),
		Current:  snap.R(1),
	})

	sideInfo = &snap.SideInfo{RealName: "test-snap-2", SnapID: "some-snap-id2", Revision: snap.R(1)}
	snaptest.MockSnap(c, snapYaml2, sideInfo)
	snapstate.Set(s.state, "test-snap-2", &snapstate.SnapState{
		Active:   true,
		Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{sideInfo}),
		Current:  snap.R(1),
	})

	var testSnap1HookCalls, testSnap2HookCalls int
	ch := make(chan struct{})
	mockHandler1 := NewMockConcurrentHandler(func() {
		ch <- struct{}{}
		testSnap1HookCalls++
	})
	mockHandler2 := NewMockConcurrentHandler(func() {
		<-ch
		testSnap2HookCalls++
	})
	s.manager.Register(regexp.MustCompile("prepare-device"), func(context *hookstate.Context) hookstate.Handler {
		if context.InstanceName() == "test-snap-1" {
			return mockHandler1
		}
		if context.InstanceName() == "test-snap-2" {
			return mockHandler2
		}
		c.Fatalf("unknown snap: %s", context.InstanceName())
		return nil
	})

	task1 := hookstate.HookTask(s.state, "test summary", hooksup1, nil)
	c.Assert(task1, NotNil)
	change1 := s.state.NewChange("kind", "summary")
	change1.AddTask(task1)

	task2 := hookstate.HookTask(s.state, "test summary", hooksup2, nil)
	c.Assert(task2, NotNil)
	change2 := s.state.NewChange("kind", "summary")
	change2.AddTask(task2)

	s.state.Unlock()

	s.settle(c)

	s.state.Lock()
	defer s.state.Unlock()

	c.Check(task1.Status(), Equals, state.DoneStatus)
	c.Check(change1.Status(), Equals, state.DoneStatus)
	c.Check(task2.Status(), Equals, state.DoneStatus)
	c.Check(change2.Status(), Equals, state.DoneStatus)
	c.Assert(testSnap1HookCalls, Equals, 1)
	c.Assert(testSnap2HookCalls, Equals, 1)
}

func (s *hookManagerSuite) TestCompatForConfigureSnapd(c *C) {
	st := s.state

	st.Lock()
	defer st.Unlock()

	task := st.NewTask("configure-snapd", "Snapd between 2.29 and 2.30 in edge insertd those tasks")
	chg := st.NewChange("configure", "configure snapd")
	chg.AddTask(task)

	st.Unlock()
	s.se.Ensure()
	s.se.Wait()
	st.Lock()

	c.Check(chg.Status(), Equals, state.DoneStatus)
	c.Check(task.Status(), Equals, state.DoneStatus)
}

func (s *hookManagerSuite) TestGracefullyWaitRunningHooksTimeout(c *C) {
	restore := hookstate.MockDefaultHookTimeout(100 * time.Millisecond)
	defer restore()

	// this works even if test-snap is not present
	s.state.Lock()
	snapstate.Set(s.state, "test-snap", nil)
	s.state.Unlock()

	quit := make(chan struct{})
	defer func() {
		quit <- struct{}{}
	}()
	didRun := make(chan bool)
	s.mockHandler.BeforeCallback = func() {
		c.Check(s.manager.NumRunningHooks(), Equals, 1)
		go func() {
			didRun <- s.manager.GracefullyWaitRunningHooks()
		}()
	}

	s.manager.RegisterHijack("configure", "test-snap", func(ctx *hookstate.Context) error {
		<-quit
		return nil
	})

	s.se.Ensure()
	select {
	case noPending := <-didRun:
		c.Check(noPending, Equals, false)
	case <-time.After(2 * time.Second):
		c.Fatal("timeout should have expired")
	}
}

func (s *hookManagerSuite) TestSnapstateOpConflict(c *C) {
	s.state.Lock()
	defer s.state.Unlock()
	_, err := snapstate.Disable(s.state, "test-snap")
	c.Assert(err, ErrorMatches, `snap "test-snap" has "kind" change in progress`)
}

func (s *hookManagerSuite) TestHookHijackingNoConflict(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	s.manager.RegisterHijack("configure", "test-snap", func(ctx *hookstate.Context) error {
		return nil
	})

	// no conflict on hijacked hooks
	_, err := snapstate.Disable(s.state, "test-snap")
	c.Assert(err, IsNil)
}

func (s *hookManagerSuite) TestEphemeralRunHook(c *C) {
	contextData := map[string]interface{}{
		"key":  "value",
		"key2": "value2",
	}
	s.testEphemeralRunHook(c, contextData)
}

func (s *hookManagerSuite) TestEphemeralRunHookNoContextData(c *C) {
	var contextData map[string]interface{} = nil
	s.testEphemeralRunHook(c, contextData)
}

func (s *hookManagerSuite) testEphemeralRunHook(c *C, contextData map[string]interface{}) {
	var hookInvokeCalled []string
	hookInvoke := func(ctx *hookstate.Context, tomb *tomb.Tomb) ([]byte, error) {
		c.Check(ctx.HookName(), Equals, "configure")
		hookInvokeCalled = append(hookInvokeCalled, ctx.HookName())

		// check that context data was set correctly
		var s string
		ctx.Lock()
		defer ctx.Unlock()
		for k, v := range contextData {
			ctx.Get(k, &s)
			c.Check(s, Equals, v)
		}
		ctx.Set("key-set-from-hook", "value-set-from-hook")

		return []byte("some output"), nil
	}
	restore := hookstate.MockRunHook(hookInvoke)
	defer restore()

	hooksup := &hookstate.HookSetup{
		Snap:     "test-snap",
		Revision: snap.R(1),
		Hook:     "configure",
	}
	context, err := s.manager.EphemeralRunHook(context.Background(), hooksup, contextData)
	c.Assert(err, IsNil)
	c.Check(hookInvokeCalled, DeepEquals, []string{"configure"})

	var value string
	context.Lock()
	context.Get("key-set-from-hook", &value)
	context.Unlock()
	c.Check(value, Equals, "value-set-from-hook")
}

func (s *hookManagerSuite) TestEphemeralRunHookNoSnap(c *C) {
	hookInvoke := func(ctx *hookstate.Context, tomb *tomb.Tomb) ([]byte, error) {
		c.Fatalf("hook should not be invoked in this test")
		return nil, nil
	}
	restore := hookstate.MockRunHook(hookInvoke)
	defer restore()

	hooksup := &hookstate.HookSetup{
		Snap:     "not-installed-snap",
		Revision: snap.R(1),
		Hook:     "configure",
	}
	contextData := map[string]interface{}{
		"key": "value",
	}
	_, err := s.manager.EphemeralRunHook(context.Background(), hooksup, contextData)
	c.Assert(err, ErrorMatches, `cannot run ephemeral hook "configure" for snap "not-installed-snap": no state entry for key`)
}

func (s *hookManagerSuite) TestEphemeralRunHookContextCanCancel(c *C) {
	tombDying := 0
	hookRunning := make(chan struct{})

	hookInvoke := func(_ *hookstate.Context, tomb *tomb.Tomb) ([]byte, error) {
		close(hookRunning)

		select {
		case <-tomb.Dying():
			tombDying++
		case <-time.After(10 * time.Second):
			c.Fatalf("hook not canceled after 10s")
		}
		return nil, nil
	}
	restore := hookstate.MockRunHook(hookInvoke)
	defer restore()

	hooksup := &hookstate.HookSetup{
		Snap:     "test-snap",
		Revision: snap.R(1),
		Hook:     "configure",
	}

	ctx, cancelFunc := context.WithCancel(context.Background())
	go func() {
		<-hookRunning
		cancelFunc()
	}()
	_, err := s.manager.EphemeralRunHook(ctx, hooksup, nil)
	c.Assert(err, IsNil)
	c.Check(tombDying, Equals, 1)
}

type parallelInstancesHookManagerSuite struct {
	baseHookManagerSuite
}

var _ = Suite(&parallelInstancesHookManagerSuite{})

func (s *parallelInstancesHookManagerSuite) SetUpTest(c *C) {
	s.commonSetUpTest(c)
	s.setUpSnap(c, "test-snap_instance", snapYaml)
}

func (s *parallelInstancesHookManagerSuite) TearDownTest(c *C) {
	s.commonTearDownTest(c)
}

func (s *parallelInstancesHookManagerSuite) TestHookTaskEnsureHookRan(c *C) {
	didRun := make(chan bool)
	s.mockHandler.BeforeCallback = func() {
		c.Check(s.manager.NumRunningHooks(), Equals, 1)
		go func() {
			didRun <- s.manager.GracefullyWaitRunningHooks()
		}()
	}
	s.se.Ensure()
	select {
	case ok := <-didRun:
		c.Check(ok, Equals, true)
	case <-time.After(5 * time.Second):
		c.Fatal("hook run should have been done by now")
	}
	s.se.Wait()

	s.state.Lock()
	defer s.state.Unlock()

	c.Check(s.context.InstanceName(), Equals, "test-snap_instance")
	c.Check(s.context.SnapRevision(), Equals, snap.R(1))
	c.Check(s.context.HookName(), Equals, "configure")

	c.Check(s.command.Calls(), DeepEquals, [][]string{{
		"snap", "run", "--hook", "configure", "-r", "1", "test-snap_instance",
	}})

	c.Check(s.mockHandler.BeforeCalled, Equals, true)
	c.Check(s.mockHandler.DoneCalled, Equals, true)
	c.Check(s.mockHandler.ErrorCalled, Equals, false)

	c.Check(s.task.Kind(), Equals, "run-hook")
	c.Check(s.task.Status(), Equals, state.DoneStatus)
	c.Check(s.change.Status(), Equals, state.DoneStatus)

	c.Check(s.manager.NumRunningHooks(), Equals, 0)
}

type componentHookManagerSuite struct {
	baseHookManagerSuite
}

var _ = Suite(&componentHookManagerSuite{})

func (s *baseHookManagerSuite) setUpComponent(c *C, instanceName string, componentName string, hookName string) {
	hooksup := &hookstate.HookSetup{
		Snap:              instanceName,
		Hook:              hookName,
		Revision:          snap.R(1),
		ComponentRevision: snap.R(1),
		Component:         componentName,
	}

	s.state.Lock()
	defer s.state.Unlock()
	s.task = hookstate.HookTask(s.state, "test-hook-task", hooksup, nil)

	s.change = s.state.NewChange("run-test-hook", "...")
	s.change.AddTask(s.task)

	snapName, instanceKey := snap.SplitInstanceName(instanceName)

	sideInfo := &snap.SideInfo{
		RealName: snapName,
		SnapID:   "some-snap-id",
		Revision: snap.R(1),
	}

	componentSideInfo := &snap.ComponentSideInfo{
		Component: naming.ComponentRef{
			SnapName:      snapName,
			ComponentName: componentName,
		},
		Revision: snap.R(1),
	}

	const componentYaml = `
component: %s+%s
type: test
`

	const snapYaml = `
name: %s
version: 1.0
components:
  %s:
    type: test
    hooks:
      %s:
`

	snapInfo := snaptest.MockSnapInstance(c, instanceName, fmt.Sprintf(snapYaml, snapName, componentName, hookName), sideInfo)
	snaptest.MockComponent(c, componentName, fmt.Sprintf(componentYaml, snapName, componentName), snapInfo)

	snapstate.Set(s.state, instanceName, &snapstate.SnapState{
		Active: true,
		Sequence: snapstatetest.NewSequenceFromRevisionSideInfos([]*sequence.RevisionSideState{{
			Snap: sideInfo,
			Components: []*sequence.ComponentState{{
				SideInfo: componentSideInfo,
				CompType: snap.TestComponent,
			}},
		}}),
		Current:     snap.R(1),
		InstanceKey: instanceKey,
	})
}

func (s *componentHookManagerSuite) SetUpTest(c *C) {
	s.commonSetUpTest(c)
	s.mockHandler = hooktest.NewMockHandler()
}

func (s *componentHookManagerSuite) TestComponentHookTaskEnsure(c *C) {
	s.setUpComponent(c, "test-snap", "test-component", "install")

	s.se.Ensure()
	s.se.Wait()

	s.state.Lock()
	defer s.state.Unlock()

	c.Check(s.command.Calls(), DeepEquals, [][]string{{
		"snap", "run", "--hook", "install", "-r", "1", "test-snap+test-component",
	}})

	c.Check(s.task.Kind(), Equals, "run-hook")
	c.Check(s.task.Status(), Equals, state.DoneStatus)
	c.Check(s.change.Status(), Equals, state.DoneStatus)

	c.Check(s.manager.NumRunningHooks(), Equals, 0)
}

func (s *componentHookManagerSuite) TestComponentHookTaskEnsureInstance(c *C) {
	s.setUpComponent(c, "test-snap_instance", "test-component", "install")

	s.se.Ensure()
	s.se.Wait()

	s.state.Lock()
	defer s.state.Unlock()

	c.Check(s.command.Calls(), DeepEquals, [][]string{{
		"snap", "run", "--hook", "install", "-r", "1", "test-snap_instance+test-component",
	}})

	fmt.Println(s.change.Err())

	c.Check(s.task.Kind(), Equals, "run-hook")
	c.Check(s.task.Status(), Equals, state.DoneStatus)
	c.Check(s.change.Status(), Equals, state.DoneStatus)

	c.Check(s.manager.NumRunningHooks(), Equals, 0)
}

func (s *componentHookManagerSuite) TestComponentHookWithoutHookIsError(c *C) {
	s.setUpComponent(c, "test-snap", "test-component", "install")

	s.state.Lock()

	var hooksup hookstate.HookSetup
	err := s.task.Get("hook-setup", &hooksup)
	c.Assert(err, IsNil)

	hooksup.Hook = "missing-hook"
	s.task.Set("hook-setup", &hooksup)

	s.state.Unlock()

	s.se.Ensure()
	s.se.Wait()

	s.state.Lock()
	defer s.state.Unlock()

	c.Check(s.task.Kind(), Equals, "run-hook")
	c.Check(s.task.Status(), Equals, state.ErrorStatus)
	c.Check(s.change.Status(), Equals, state.ErrorStatus)
	checkTaskLogContains(c, s.task, `.*component "test-snap\+test-component" has no "missing-hook" hook.*`)
}
