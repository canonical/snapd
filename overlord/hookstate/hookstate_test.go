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
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/overlord/hookstate"
	"github.com/snapcore/snapd/overlord/hookstate/hooktest"
	"github.com/snapcore/snapd/overlord/snapstate"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/snap/snaptest"
	"github.com/snapcore/snapd/testutil"
)

func TestHookManager(t *testing.T) { TestingT(t) }

type hookManagerSuite struct {
	testutil.BaseTest

	state       *state.State
	manager     *hookstate.HookManager
	context     *hookstate.Context
	mockHandler *hooktest.MockHandler
	task        *state.Task
	change      *state.Change
	command     *testutil.MockCmd
}

var _ = Suite(&hookManagerSuite{})

var snapYaml = `
name: test-snap
version: 1.0
hooks:
    configure:
    prepare-device:
`
var snapContents = ""

func (s *hookManagerSuite) SetUpTest(c *C) {
	s.BaseTest.SetUpTest(c)

	dirs.SetRootDir(c.MkDir())
	s.state = state.New(nil)
	manager, err := hookstate.Manager(s.state)
	c.Assert(err, IsNil)
	s.manager = manager

	hooksup := &hookstate.HookSetup{
		Snap:     "test-snap",
		Hook:     "configure",
		Revision: snap.R(1),
	}

	initialContext := map[string]interface{}{
		"test-key": "test-value",
	}

	s.state.Lock()
	s.task = hookstate.HookTask(s.state, "test summary", hooksup, initialContext)
	c.Assert(s.task, NotNil, Commentf("Expected HookTask to return a task"))

	s.change = s.state.NewChange("kind", "summary")
	s.change.AddTask(s.task)

	sideInfo := &snap.SideInfo{RealName: "test-snap", SnapID: "some-snap-id", Revision: snap.R(1)}
	snaptest.MockSnap(c, snapYaml, snapContents, sideInfo)
	snapstate.Set(s.state, "test-snap", &snapstate.SnapState{
		Active:   true,
		Sequence: []*snap.SideInfo{sideInfo},
		Current:  snap.R(1),
	})
	s.state.Unlock()

	s.command = testutil.MockCommand(c, "snap", "")

	s.context = nil
	s.mockHandler = hooktest.NewMockHandler()
	s.manager.Register(regexp.MustCompile("configure"), func(context *hookstate.Context) hookstate.Handler {
		s.context = context
		return s.mockHandler
	})
	s.AddCleanup(hookstate.MockErrtrackerReport(func(string, string, string, map[string]string) (string, error) {
		return "", nil
	}))
}

func (s *hookManagerSuite) TearDownTest(c *C) {
	s.BaseTest.TearDownTest(c)

	s.manager.Stop()
	dirs.SetRootDir("")
}

func (s *hookManagerSuite) TestSmoke(c *C) {
	s.manager.Ensure()
	s.manager.Wait()
}

func (s *hookManagerSuite) TestHookSetupJsonMarshal(c *C) {
	hookSetup := &hookstate.HookSetup{Snap: "snap-name", Revision: snap.R(1), Hook: "hook-name"}
	out, err := json.Marshal(hookSetup)
	c.Assert(err, IsNil)
	c.Check(string(out), Equals, "{\"snap\":\"snap-name\",\"revision\":\"1\",\"hook\":\"hook-name\"}")
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
	s.manager.Ensure()
	s.manager.Wait()

	s.state.Lock()
	defer s.state.Unlock()

	c.Assert(s.context, NotNil, Commentf("Expected handler generator to be called with a valid context"))
	c.Check(s.context.SnapName(), Equals, "test-snap")
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
}

func (s *hookManagerSuite) TestHookTaskInitializesContext(c *C) {
	s.manager.Ensure()
	s.manager.Wait()

	var value string
	c.Assert(s.context, NotNil, Commentf("Expected handler generator to be called with a valid context"))
	s.context.Lock()
	defer s.context.Unlock()
	c.Check(s.context.Get("test-key", &value), IsNil, Commentf("Expected context to be initialized"))
	c.Check(value, Equals, "test-value")
}

func (s *hookManagerSuite) TestHookTaskHandlesHookError(c *C) {
	// Force the snap command to exit 1, and print something to stderr
	s.command = testutil.MockCommand(
		c, "snap", ">&2 echo 'hook failed at user request'; exit 1")

	s.manager.Ensure()
	s.manager.Wait()

	s.state.Lock()
	defer s.state.Unlock()

	c.Check(s.mockHandler.BeforeCalled, Equals, true)
	c.Check(s.mockHandler.DoneCalled, Equals, false)
	c.Check(s.mockHandler.ErrorCalled, Equals, true)

	c.Check(s.task.Kind(), Equals, "run-hook")
	c.Check(s.task.Status(), Equals, state.ErrorStatus)
	c.Check(s.change.Status(), Equals, state.ErrorStatus)
	checkTaskLogContains(c, s.task, ".*failed at user request.*")
}

func (s *hookManagerSuite) TestHookTaskHandleIgnoreErrorWorks(c *C) {
	s.state.Lock()
	var hooksup hookstate.HookSetup
	s.task.Get("hook-setup", &hooksup)
	hooksup.IgnoreError = true
	s.task.Set("hook-setup", &hooksup)
	s.state.Unlock()

	// Force the snap command to exit 1, and print something to stderr
	s.command = testutil.MockCommand(
		c, "snap", ">&2 echo 'hook failed at user request'; exit 1")

	s.manager.Ensure()
	s.manager.Wait()

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

func (s *hookManagerSuite) TestHookTaskEnforcesTimeout(c *C) {
	var hooksup hookstate.HookSetup

	s.state.Lock()
	s.task.Get("hook-setup", &hooksup)
	hooksup.Timeout = time.Duration(200 * time.Millisecond)
	s.task.Set("hook-setup", &hooksup)
	s.state.Unlock()

	// Force the snap command to hang
	s.command = testutil.MockCommand(c, "snap", "while true; do sleep 1; done")

	s.manager.Ensure()
	completed := make(chan struct{})
	go func() {
		s.manager.Wait()
		close(completed)
	}()

	s.state.Lock()
	s.state.Unlock()
	s.manager.Ensure()
	<-completed

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

func (s *hookManagerSuite) TestHookTaskEnforcesMaxWaitTime(c *C) {
	var hooksup hookstate.HookSetup

	s.state.Lock()
	s.task.Get("hook-setup", &hooksup)
	hooksup.Timeout = time.Duration(200 * time.Millisecond)
	s.task.Set("hook-setup", &hooksup)
	s.state.Unlock()

	// Force the snap command to hang
	s.command = testutil.MockCommand(c, "snap", "while true; do sleep 1; done")

	// simulate a process that can not be killed
	restore := hookstate.MockSyscallKill(func(int, syscall.Signal) error {
		return nil
	})
	defer restore()

	restore = hookstate.MockCmdWaitTimeout(100 * time.Millisecond)
	defer restore()

	s.manager.Ensure()
	completed := make(chan struct{})
	go func() {
		s.manager.Wait()
		close(completed)
	}()

	s.state.Lock()
	s.state.Unlock()
	s.manager.Ensure()
	<-completed

	s.state.Lock()
	defer s.state.Unlock()

	c.Check(s.mockHandler.BeforeCalled, Equals, true)
	c.Check(s.mockHandler.DoneCalled, Equals, false)
	c.Check(s.mockHandler.ErrorCalled, Equals, true)
	c.Check(s.mockHandler.Err, ErrorMatches, `.*exceeded maximum runtime of 200ms, but did not stop`)

	c.Check(s.task.Kind(), Equals, "run-hook")
	c.Check(s.task.Status(), Equals, state.ErrorStatus)
	c.Check(s.change.Status(), Equals, state.ErrorStatus)
	checkTaskLogContains(c, s.task, `.*exceeded maximum runtime of 200ms, but did not stop`)
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
	s.command = testutil.MockCommand(c, "snap", "while true; do sleep 1; done")

	s.manager.Ensure()
	completed := make(chan struct{})
	go func() {
		s.manager.Wait()
		close(completed)
	}()

	s.state.Lock()
	s.state.Unlock()
	s.manager.Ensure()
	<-completed

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
	s.command = testutil.MockCommand(c, "snap", "while true; do sleep 1; done")

	s.manager.Ensure()
	completed := make(chan struct{})
	go func() {
		s.manager.Wait()
		close(completed)
	}()

	// Abort the change, which should kill the hanging hook, and wait for the
	// task to complete.
	s.state.Lock()
	s.change.Abort()
	s.state.Unlock()
	s.manager.Ensure()
	<-completed

	s.state.Lock()
	defer s.state.Unlock()

	c.Check(s.mockHandler.BeforeCalled, Equals, true)
	c.Check(s.mockHandler.DoneCalled, Equals, false)
	c.Check(s.mockHandler.ErrorCalled, Equals, true)
	c.Check(s.mockHandler.Err, ErrorMatches, ".*hook aborted.*")

	c.Check(s.task.Kind(), Equals, "run-hook")
	c.Check(s.task.Status(), Equals, state.ErrorStatus)
	c.Check(s.change.Status(), Equals, state.ErrorStatus)
	checkTaskLogContains(c, s.task, `.*hook aborted.*`)
}

func (s *hookManagerSuite) TestHookTaskCorrectlyIncludesContext(c *C) {
	// Force the snap command to exit with a failure and print to stderr so we
	// can catch and verify it.
	s.command = testutil.MockCommand(
		c, "snap", ">&2 echo \"SNAP_CONTEXT=$SNAP_CONTEXT\"; exit 1")

	s.manager.Ensure()
	s.manager.Wait()

	s.state.Lock()
	defer s.state.Unlock()

	c.Check(s.mockHandler.BeforeCalled, Equals, true)
	c.Check(s.mockHandler.DoneCalled, Equals, false)
	c.Check(s.mockHandler.ErrorCalled, Equals, true)

	c.Check(s.task.Kind(), Equals, "run-hook")
	c.Check(s.task.Status(), Equals, state.ErrorStatus)
	c.Check(s.change.Status(), Equals, state.ErrorStatus)
	checkTaskLogContains(c, s.task, `.*SNAP_CONTEXT=\S+`)
}

func (s *hookManagerSuite) TestHookTaskHandlerBeforeError(c *C) {
	s.mockHandler.BeforeError = true

	s.manager.Ensure()
	s.manager.Wait()

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

	s.manager.Ensure()
	s.manager.Wait()

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
	s.command = testutil.MockCommand(c, "snap", "exit 1")

	s.manager.Ensure()
	s.manager.Wait()

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

func (s *hookManagerSuite) TestHookWithoutHandlerIsError(c *C) {
	hooksup := &hookstate.HookSetup{
		Snap:     "test-snap",
		Hook:     "prepare-device",
		Revision: snap.R(1),
	}
	s.state.Lock()
	s.task.Set("hook-setup", hooksup)
	s.state.Unlock()

	s.manager.Ensure()
	s.manager.Wait()

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

	s.manager.Ensure()
	s.manager.Wait()

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

	s.manager.Ensure()
	s.manager.Wait()

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

	s.manager.Ensure()
	s.manager.Wait()

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
	err := os.MkdirAll(filepath.Dir(coreSnapCmdPath), 0755)
	c.Assert(err, IsNil)
	s.command = testutil.MockCommand(c, coreSnapCmdPath, "")

	r := hookstate.MockReadlink(func(p string) (string, error) {
		c.Assert(p, Equals, "/proc/self/exe")
		return filepath.Join(dirs.SnapMountDir, "core/12/usr/lib/snapd/snapd"), nil
	})
	defer r()

	s.manager.Ensure()
	s.manager.Wait()

	s.state.Lock()
	defer s.state.Unlock()

	c.Assert(s.context, NotNil, Commentf("Expected handler generator to be called with a valid context"))
	c.Check(s.command.Calls(), DeepEquals, [][]string{{
		"snap", "run", "--hook", "configure", "-r", "1", "test-snap",
	}})

}

func (s *hookManagerSuite) TestHookTaskHandlerReportsErrorIfRequested(c *C) {
	s.state.Lock()
	var hooksup hookstate.HookSetup
	s.task.Get("hook-setup", &hooksup)
	hooksup.TrackError = true
	s.task.Set("hook-setup", &hooksup)
	s.state.Unlock()

	errtrackerCalled := false
	hookstate.MockErrtrackerReport(func(snap, errmsg, dupSig string, extra map[string]string) (string, error) {
		c.Check(snap, Equals, "test-snap")
		c.Check(errmsg, Equals, "hook configure in snap \"test-snap\" failed: hook failed at user request")
		c.Check(dupSig, Equals, "hook:test-snap:configure:exit status 1\nhook failed at user request\n")

		errtrackerCalled = true
		return "some-oopsid", nil
	})

	// Force the snap command to exit 1, and print something to stderr
	s.command = testutil.MockCommand(
		c, "snap", ">&2 echo 'hook failed at user request'; exit 1")

	s.manager.Ensure()
	s.manager.Wait()

	s.state.Lock()
	defer s.state.Unlock()

	c.Check(errtrackerCalled, Equals, true)
}

func (s *hookManagerSuite) TestHookTasksForSameSnapAreSerialized(c *C) {
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

	for i := 0; i < 50; i++ {
		s.manager.Ensure()
		s.manager.Wait()
	}

	s.state.Lock()
	defer s.state.Unlock()

	c.Check(s.task.Kind(), Equals, "run-hook")
	c.Check(s.task.Status(), Equals, state.DoneStatus)
	c.Check(s.change.Status(), Equals, state.DoneStatus)

	for i := 0; i < len(tasks); i++ {
		c.Check(tasks[i].Kind(), Equals, "run-hook")
		c.Check(tasks[i].Status(), Equals, state.DoneStatus)
	}
	c.Assert(atomic.LoadInt32(&s.mockHandler.TotalExecutions), Equals, int32(1+len(tasks)))
}
