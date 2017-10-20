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
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sync/atomic"
	"testing"
	"time"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/overlord"
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

	o           *overlord.Overlord
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

var snapContents = ""

func (s *hookManagerSuite) SetUpTest(c *C) {
	s.BaseTest.SetUpTest(c)

	dirs.SetRootDir(c.MkDir())
	s.o = overlord.Mock()
	s.state = s.o.State()
	manager, err := hookstate.Manager(s.state)
	c.Assert(err, IsNil)
	s.manager = manager
	s.o.AddManager(s.manager)

	s.AddCleanup(snap.MockSanitizePlugsSlots(func(snapInfo *snap.Info) {}))

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
	s.AddCleanup(s.command.Restore)

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

func (s *hookManagerSuite) settle(c *C) {
	err := s.o.Settle(5 * time.Second)
	c.Assert(err, IsNil)
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
	cmd := testutil.MockCommand(
		c, "snap", ">&2 echo 'hook failed at user request'; exit 1")
	defer cmd.Restore()

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
	cmd := testutil.MockCommand(
		c, "snap", ">&2 echo 'hook failed at user request'; exit 1")
	defer cmd.Restore()

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
	cmd := testutil.MockCommand(c, "snap", "while true; do sleep 1; done")
	defer cmd.Restore()

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

func (s *hookManagerSuite) TestHookTaskEnforcesDefaultTimeout(c *C) {
	restore := hookstate.MockDefaultHookTimeout(150 * time.Millisecond)
	defer restore()

	// Force the snap command to hang
	cmd := testutil.MockCommand(c, "snap", "while true; do sleep 1; done")
	defer cmd.Restore()

	s.manager.Ensure()
	s.manager.Wait()

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
	cmd := testutil.MockCommand(c, "snap", "while true; do sleep 1; done")
	defer cmd.Restore()

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
	c.Check(s.mockHandler.Err, ErrorMatches, "<aborted>")

	c.Check(s.task.Kind(), Equals, "run-hook")
	c.Check(s.task.Status(), Equals, state.ErrorStatus)
	c.Check(s.change.Status(), Equals, state.ErrorStatus)
	checkTaskLogContains(c, s.task, `run hook "[^"]*": <aborted>`)
}

func (s *hookManagerSuite) TestHookTaskCorrectlyIncludesContext(c *C) {
	// Force the snap command to exit with a failure and print to stderr so we
	// can catch and verify it.
	cmd := testutil.MockCommand(
		c, "snap", ">&2 echo \"SNAP_COOKIE=$SNAP_COOKIE\"; exit 1")
	defer cmd.Restore()

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
	checkTaskLogContains(c, s.task, `.*SNAP_COOKIE=\S+`)
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
	cmd := testutil.MockCommand(c, "snap", "exit 1")
	defer cmd.Restore()

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

func (s *hookManagerSuite) TestOptionalHookWithMissingHandler(c *C) {
	hooksup := &hookstate.HookSetup{
		Snap:     "test-snap",
		Hook:     "missing-hook-and-no-handler",
		Optional: true,
	}
	s.state.Lock()
	s.task.Set("hook-setup", hooksup)
	s.state.Unlock()

	s.manager.Ensure()
	s.manager.Wait()

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
	cmd := testutil.MockCommand(c, coreSnapCmdPath, "")
	defer cmd.Restore()

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
	c.Check(cmd.Calls(), DeepEquals, [][]string{{
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
	cmd := testutil.MockCommand(
		c, "snap", ">&2 echo 'hook failed at user request'; exit 1")
	defer cmd.Restore()

	s.manager.Ensure()
	s.manager.Wait()

	s.state.Lock()
	defer s.state.Unlock()

	c.Check(errtrackerCalled, Equals, true)
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

func (h *MockConcurrentHandler) Error(err error) error {
	return nil
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
	info := snaptest.MockSnap(c, snapYaml1, snapContents, sideInfo)
	c.Assert(info.Hooks, HasLen, 1)
	snapstate.Set(s.state, "test-snap-1", &snapstate.SnapState{
		Active:   true,
		Sequence: []*snap.SideInfo{sideInfo},
		Current:  snap.R(1),
	})

	sideInfo = &snap.SideInfo{RealName: "test-snap-2", SnapID: "some-snap-id2", Revision: snap.R(1)}
	snaptest.MockSnap(c, snapYaml2, snapContents, sideInfo)
	snapstate.Set(s.state, "test-snap-2", &snapstate.SnapState{
		Active:   true,
		Sequence: []*snap.SideInfo{sideInfo},
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
		if context.SnapName() == "test-snap-1" {
			return mockHandler1
		}
		if context.SnapName() == "test-snap-2" {
			return mockHandler2
		}
		c.Fatalf("unknown snap: %s", context.SnapName())
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
