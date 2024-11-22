// -*- Mode: Go; indent-tabs-mode: t -*-
/*
 * Copyright (C) 2024 Canonical Ltd
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

package confdbstate_test

import (
	"errors"
	"strings"
	"time"

	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/interfaces"
	"github.com/snapcore/snapd/interfaces/ifacetest"
	"github.com/snapcore/snapd/overlord"
	"github.com/snapcore/snapd/overlord/confdbstate"
	"github.com/snapcore/snapd/overlord/hookstate"
	"github.com/snapcore/snapd/overlord/ifacestate/ifacerepo"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/testutil"

	. "gopkg.in/check.v1"
)

type hookHandlerSuite struct {
	state *state.State

	repo *interfaces.Repository
}

var _ = Suite(&hookHandlerSuite{})

func (s *hookHandlerSuite) SetUpTest(c *C) {
	dirs.SetRootDir(c.MkDir())
	s.state = overlord.Mock().State()

	s.state.Lock()
	defer s.state.Unlock()

	s.repo = interfaces.NewRepository()
	ifacerepo.Replace(s.state, s.repo)

	confdbIface := &ifacetest.TestInterface{InterfaceName: "confdb"}
	err := s.repo.AddInterface(confdbIface)
	c.Assert(err, IsNil)

	const coreYaml = `name: core
version: 1
type: os
slots:
  confdb-slot:
    interface: confdb
`
	info := mockInstalledSnap(c, s.state, coreYaml, nil)
	coreSet, err := interfaces.NewSnapAppSet(info, nil)
	c.Assert(err, IsNil)

	err = s.repo.AddAppSet(coreSet)
	c.Assert(err, IsNil)

	snapYaml := `name: test-snap
version: 1
type: app
plugs:
  setup:
    interface: confdb
    account: my-acc
    view: network/setup-wifi
`

	info = mockInstalledSnap(c, s.state, snapYaml, nil)
	appSet, err := interfaces.NewSnapAppSet(info, nil)
	c.Assert(err, IsNil)
	err = s.repo.AddAppSet(appSet)
	c.Assert(err, IsNil)

	ref := &interfaces.ConnRef{
		PlugRef: interfaces.PlugRef{Snap: "test-snap", Name: "setup"},
		SlotRef: interfaces.SlotRef{Snap: "core", Name: "confdb-slot"},
	}

	_, err = s.repo.Connect(ref, nil, nil, nil, nil, nil)
	c.Assert(err, IsNil)
}

func setTransaction(t *state.Task, tx *confdbstate.Transaction) {
	t.Set("confdb-transaction", tx)
}

func (s *hookHandlerSuite) TestViewChangeHookOk(c *C) {
	s.state.Lock()
	hooksup := &hookstate.HookSetup{
		Snap: "test-snap",
		Hook: "change-view-setup",
	}

	tx, err := confdbstate.NewTransaction(s.state, "my-acc", "network")
	c.Assert(err, IsNil)

	t := s.state.NewTask("task", "")
	setTransaction(t, tx)

	ctx, err := hookstate.NewContext(t, s.state, hooksup, nil, "")
	c.Assert(err, IsNil)

	handler := confdbstate.ChangeViewHandlerGenerator(ctx)
	s.state.Unlock()

	c.Assert(err, IsNil)
	err = handler.Done()
	c.Assert(err, IsNil)
}

func (s *hookHandlerSuite) TestChangeViewHookRejectsChanges(c *C) {
	s.state.Lock()
	hooksup := &hookstate.HookSetup{
		Snap: "test-snap",
		Hook: "change-view-setup",
	}

	tx, err := confdbstate.NewTransaction(s.state, "my-acc", "network")
	c.Assert(err, IsNil)
	tx.Abort("my-snap", "don't like")

	t := s.state.NewTask("task", "")
	setTransaction(t, tx)

	ctx, err := hookstate.NewContext(t, s.state, hooksup, nil, "")
	c.Assert(err, IsNil)

	handler := confdbstate.ChangeViewHandlerGenerator(ctx)
	s.state.Unlock()

	err = handler.Done()
	c.Assert(err, ErrorMatches, `cannot change confdb my-acc/network: my-snap rejected changes: don't like`)
}

func (s *hookHandlerSuite) TestSaveViewHookOk(c *C) {
	s.state.Lock()
	hooksup := &hookstate.HookSetup{
		Snap: "test-snap",
		Hook: "save-view-setup",
	}

	tx, err := confdbstate.NewTransaction(s.state, "my-acc", "network")
	c.Assert(err, IsNil)

	t := s.state.NewTask("task", "")
	setTransaction(t, tx)

	ctx, err := hookstate.NewContext(t, s.state, hooksup, nil, "")
	c.Assert(err, IsNil)

	handler := confdbstate.SaveViewHandlerGenerator(ctx)
	s.state.Unlock()

	err = handler.Done()
	c.Assert(err, IsNil)
}

func (s *hookHandlerSuite) TestSaveViewHookErrorRollsBackSaves(c *C) {
	s.state.Lock()
	chg := s.state.NewChange("my-change", "")
	commitTask := s.state.NewTask("commit-confdb-tx", "")
	chg.AddTask(commitTask)

	tx, err := confdbstate.NewTransaction(s.state, "my-acc", "network")
	c.Assert(err, IsNil)
	setTransaction(commitTask, tx)

	err = tx.Set("foo", "bar")
	c.Assert(err, IsNil)

	// the first save-view hook is done
	hooksup := &hookstate.HookSetup{
		Snap: "first-snap",
		Hook: "save-view-setup",
	}
	firstTask := s.state.NewTask("run-hook", "")
	chg.AddTask(firstTask)
	firstTask.SetStatus(state.DoneStatus)
	firstTask.Set("hook-setup", hooksup)

	// Error looks for a non run-hook task in order to stop
	prereq := s.state.NewTask("other", "")
	chg.AddTask(prereq)
	firstTask.WaitFor(prereq)

	// setup the second save-view hook as the one that fails
	hooksup = &hookstate.HookSetup{
		Snap: "second-snap",
		Hook: "save-view-setup",
	}
	secondTask := s.state.NewTask("run-hook", "")
	chg.AddTask(secondTask)
	secondTask.WaitFor(firstTask)
	secondTask.SetStatus(state.DoingStatus)
	secondTask.Set("hook-setup", hooksup)
	secondTask.Set("commit-task", commitTask.ID())

	ctx, err := hookstate.NewContext(secondTask, s.state, hooksup, nil, "")
	c.Assert(err, IsNil)

	handler := confdbstate.SaveViewHandlerGenerator(ctx)
	s.state.Unlock()

	savingErr := errors.New("failed to save")
	ignore, err := handler.Error(savingErr)
	// Error creates tasks to roll back the previous saves so it ignores this error
	c.Assert(err, IsNil)
	c.Assert(ignore, Equals, true)

	// we haven't rolled back yet, so we're still not surfacing the error
	err = handler.Done()
	c.Assert(err, IsNil)

	s.state.Lock()
	// the transaction has been cleared
	tx, _, err = confdbstate.GetStoredTransaction(secondTask)
	c.Assert(err, IsNil)
	_, err = tx.Get("foo")
	c.Assert(err, ErrorMatches, "no value was found under path \"foo\"")

	halts := secondTask.HaltTasks()
	c.Assert(halts, HasLen, 1)
	cleanupTask := halts[0]

	hooksup = nil
	err = cleanupTask.Get("hook-setup", &hooksup)
	c.Assert(err, IsNil)

	// the first cleanup task will run for the errored hook
	c.Assert(hooksup.Hook, Equals, "save-view-setup")
	c.Assert(hooksup.Snap, Equals, "second-snap")

	// only the last cleanup task has the original error to signal that it should
	// be surfaced
	var origErr string
	err = cleanupTask.Get("original-error", &origErr)
	c.Assert(err, testutil.ErrorIs, state.ErrNoState)

	cleanupTask = cleanupTask.HaltTasks()[0]
	hooksup = nil
	err = cleanupTask.Get("hook-setup", &hooksup)
	c.Assert(err, IsNil)

	// the second cleanup task will run for the completed hook
	c.Assert(hooksup.Hook, Equals, "save-view-setup")
	c.Assert(hooksup.Snap, Equals, "first-snap")

	// the original error was saved so we can surface it after rolling back
	err = cleanupTask.Get("original-error", &origErr)
	c.Assert(err, IsNil)
	c.Assert(origErr, Equals, savingErr.Error())

	ctx, err = hookstate.NewContext(cleanupTask, s.state, hooksup, nil, "")
	c.Assert(err, IsNil)
	s.state.Unlock()

	handler = confdbstate.SaveViewHandlerGenerator(ctx)
	err = handler.Done()
	c.Assert(err, ErrorMatches, savingErr.Error())
}

func (s *hookHandlerSuite) TestSaveViewHookErrorHoldsTasks(c *C) {
	s.state.Lock()
	chg := s.state.NewChange("my-change", "")
	commitTask := s.state.NewTask("commit-confdb-tx", "")
	chg.AddTask(commitTask)

	tx, err := confdbstate.NewTransaction(s.state, "my-acc", "network")
	c.Assert(err, IsNil)
	setTransaction(commitTask, tx)

	err = tx.Set("foo", "bar")
	c.Assert(err, IsNil)

	// the first save-view hook will fail
	hooksup := &hookstate.HookSetup{
		Snap: "first-snap",
		Hook: "save-view-setup",
	}
	firstTask := s.state.NewTask("run-hook", "1st save-view")
	chg.AddTask(firstTask)
	firstTask.SetStatus(state.DoingStatus)
	firstTask.Set("hook-setup", hooksup)
	firstTask.Set("commit-task", commitTask.ID())

	// Error looks for a non run-hook task in order to stop
	prereq := s.state.NewTask("other", "")
	chg.AddTask(prereq)
	firstTask.WaitFor(prereq)

	hooksup = &hookstate.HookSetup{
		Snap: "second-snap",
		Hook: "save-view-setup",
	}
	secondTask := s.state.NewTask("run-hook", "2nd save-view")
	chg.AddTask(secondTask)
	secondTask.WaitFor(firstTask)
	secondTask.SetStatus(state.DoStatus)
	secondTask.Set("hook-setup", hooksup)

	ctx, err := hookstate.NewContext(firstTask, s.state, hooksup, nil, "")
	c.Assert(err, IsNil)

	handler := confdbstate.SaveViewHandlerGenerator(ctx)

	s.state.Unlock()
	ignore, err := handler.Error(errors.New("failed to save"))
	c.Assert(err, IsNil)
	c.Assert(ignore, Equals, true)
	s.state.Lock()

	halts := firstTask.HaltTasks()
	c.Assert(halts, HasLen, 2)

	// the rollback task for the failed hook
	rollbackTask := halts[1]
	// it was inserted between the failed task and the second save-view task
	c.Assert(rollbackTask.HaltTasks(), HasLen, 1)
	c.Assert(rollbackTask.HaltTasks()[0].ID(), Equals, secondTask.ID())

	c.Assert(rollbackTask.Kind(), Equals, "run-hook")
	c.Assert(rollbackTask.Status(), Equals, state.DoStatus)
	err = rollbackTask.Get("hook-setup", &hooksup)
	c.Assert(err, IsNil)
	c.Assert(hooksup.Hook, Equals, "save-view-setup")
	c.Assert(hooksup.Snap, Equals, "first-snap")
}

func (s *confdbTestSuite) TestManagerOk(c *C) {
	runner := s.o.TaskRunner()
	hookMgr, err := hookstate.Manager(s.state, runner)
	c.Assert(err, IsNil)

	mgr := confdbstate.Manager(s.state, hookMgr, runner)
	s.o.AddManager(mgr)

	err = s.o.Settle(5 * time.Second)
	c.Assert(err, IsNil)
}

func (s *confdbTestSuite) TestSetAndUnsetOngoingTransactionHelpers(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	var commitTasks map[string]string
	err := s.state.Get("confdb-commit-tasks", &commitTasks)
	c.Assert(err, testutil.ErrorIs, &state.NoStateError{})

	err = confdbstate.SetOngoingTransaction(s.state, "my-acc", "my-confdb", "1")
	c.Assert(err, IsNil)

	// can't overwrite an ongoing commit task, since that could hide errors
	err = confdbstate.SetOngoingTransaction(s.state, "my-acc", "my-confdb", "3")
	c.Assert(err, ErrorMatches, `internal error: cannot set task "3" as ongoing commit task for confdb my-acc/my-confdb: already have "1"`)

	err = confdbstate.SetOngoingTransaction(s.state, "other-acc", "other-confdb", "2")
	c.Assert(err, IsNil)

	err = s.state.Get("confdb-commit-tasks", &commitTasks)
	c.Assert(err, IsNil)
	c.Assert(commitTasks["my-acc/my-confdb"], Equals, "1")

	err = confdbstate.UnsetOngoingTransaction(s.state, "my-acc", "my-confdb")
	c.Assert(err, IsNil)

	// unsetting non-existing key is fine
	err = confdbstate.UnsetOngoingTransaction(s.state, "my-acc", "my-confdb")
	c.Assert(err, IsNil)

	err = s.state.Get("confdb-commit-tasks", &commitTasks)
	c.Assert(err, IsNil)
	c.Assert(commitTasks["other-acc/other-confdb"], Equals, "2")

	err = confdbstate.UnsetOngoingTransaction(s.state, "other-acc", "other-confdb")
	c.Assert(err, IsNil)

	err = s.state.Get("confdb-commit-tasks", &commitTasks)
	c.Assert(err, testutil.ErrorIs, &state.NoStateError{})

	// unsetting non-existing key is still fine when there's no map at all
	err = confdbstate.UnsetOngoingTransaction(s.state, "my-acc", "my-confdb")
	c.Assert(err, IsNil)
}

func (s *confdbTestSuite) TestCommitTransaction(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	chg := s.state.NewChange("test", "")
	t := s.state.NewTask("commit-confdb-tx", "")
	chg.AddTask(t)

	// attach a transaction with some changes to the commit task
	tx, err := confdbstate.NewTransaction(s.state, s.devAccID, "network")
	c.Assert(err, IsNil)

	err = tx.Set("wifi.ssid", "foo")
	c.Assert(err, IsNil)

	setTransaction(t, tx)

	s.state.Unlock()
	err = s.o.Settle(testutil.HostScaledTimeout(5 * time.Second))
	s.state.Lock()
	c.Assert(err, IsNil)

	c.Assert(t.Status(), Equals, state.DoneStatus, Commentf(strings.Join(t.Log(), "\n")))

	tx, _, err = confdbstate.GetStoredTransaction(t)
	c.Assert(err, IsNil)

	// clearing would remove non-committed changes, so if we read the set value
	// it's because it has been successfully committed
	err = tx.Clear(s.state)
	c.Assert(err, IsNil)

	val, err := tx.Get("wifi.ssid")
	c.Assert(err, IsNil)
	c.Assert(val, Equals, "foo")
}

func (s *confdbTestSuite) TestClearOngoingTransaction(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	chg := s.state.NewChange("test", "")
	commitTask := s.state.NewTask("commit-confdb-tx", "")
	commitTask.SetStatus(state.DoneStatus)
	chg.AddTask(commitTask)

	tx, err := confdbstate.NewTransaction(s.state, s.devAccID, "network")
	c.Assert(err, IsNil)
	setTransaction(commitTask, tx)

	t := s.state.NewTask("clear-confdb-tx", "")
	chg.AddTask(t)
	t.Set("commit-task", commitTask.ID())

	confdbstate.SetOngoingTransaction(s.state, s.devAccID, "network", commitTask.ID())

	var commitTasks map[string]string
	err = s.state.Get("confdb-commit-tasks", &commitTasks)
	c.Assert(err, IsNil)

	s.state.Unlock()
	err = s.o.Settle(testutil.HostScaledTimeout(5 * time.Second))
	s.state.Lock()
	c.Assert(err, IsNil)
	c.Assert(t.Status(), Equals, state.DoneStatus, Commentf(strings.Join(t.Log(), "\n")))

	commitTasks = nil
	err = s.state.Get("confdb-commit-tasks", &commitTasks)
	c.Assert(err, testutil.ErrorIs, &state.NoStateError{})
}

func (s *confdbTestSuite) TestClearTransactionOnError(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	chg := s.state.NewChange("test", "")
	clearTask := s.state.NewTask("clear-confdb-tx-on-error", "")
	chg.AddTask(clearTask)

	commitTask := s.state.NewTask("commit-confdb-tx", "")
	chg.AddTask(commitTask)
	commitTask.WaitFor(clearTask)
	clearTask.Set("commit-task", commitTask.ID())

	tx, err := confdbstate.NewTransaction(s.state, s.devAccID, "network")
	c.Assert(err, IsNil)

	// the schema will reject this, so the commit will fail
	err = tx.Set("foo", "bar")
	c.Assert(err, IsNil)
	setTransaction(commitTask, tx)

	// add this transaction to the state
	confdbstate.SetOngoingTransaction(s.state, s.devAccID, "network", commitTask.ID())

	s.state.Unlock()
	err = s.o.Settle(testutil.HostScaledTimeout(5 * time.Second))
	s.state.Lock()
	c.Assert(err, IsNil)

	c.Assert(chg.Status(), Equals, state.ErrorStatus)
	c.Assert(commitTask.Status(), Equals, state.ErrorStatus)
	c.Assert(clearTask.Status(), Equals, state.UndoneStatus)
	c.Assert(strings.Join(commitTask.Log(), "\n"), Matches, ".*ERROR cannot accept top level element: map contains unexpected key \"foo\"")

	// no ongoing confdb transaction
	var commitTasks map[string]string
	err = s.state.Get("confdb-commit-tasks", &commitTasks)
	c.Assert(err, testutil.ErrorIs, &state.NoStateError{})
}
