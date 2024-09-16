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

package registrystate_test

import (
	"errors"

	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/interfaces"
	"github.com/snapcore/snapd/interfaces/ifacetest"
	"github.com/snapcore/snapd/overlord"
	"github.com/snapcore/snapd/overlord/hookstate"
	"github.com/snapcore/snapd/overlord/ifacestate/ifacerepo"
	"github.com/snapcore/snapd/overlord/registrystate"
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

	regIface := &ifacetest.TestInterface{InterfaceName: "registry"}
	err := s.repo.AddInterface(regIface)
	c.Assert(err, IsNil)

	const coreYaml = `name: core
version: 1
type: os
slots:
  registry-slot:
    interface: registry
`
	info := mockInstalledSnap(c, s.state, coreYaml, "")
	coreSet, err := interfaces.NewSnapAppSet(info, nil)
	c.Assert(err, IsNil)

	err = s.repo.AddAppSet(coreSet)
	c.Assert(err, IsNil)

	snapYaml := `name: test-snap
version: 1
type: app
plugs:
  setup:
    interface: registry
    account: my-acc
    view: network/setup-wifi
`

	info = mockInstalledSnap(c, s.state, snapYaml, "")
	appSet, err := interfaces.NewSnapAppSet(info, nil)
	c.Assert(err, IsNil)
	err = s.repo.AddAppSet(appSet)
	c.Assert(err, IsNil)

	ref := &interfaces.ConnRef{
		PlugRef: interfaces.PlugRef{Snap: "test-snap", Name: "setup"},
		SlotRef: interfaces.SlotRef{Snap: "core", Name: "registry-slot"},
	}

	_, err = s.repo.Connect(ref, nil, nil, nil, nil, nil)
	c.Assert(err, IsNil)
}

func (s *hookHandlerSuite) TestViewChangeHookOk(c *C) {
	s.state.Lock()
	hooksup := &hookstate.HookSetup{
		Snap: "test-snap",
		Hook: "view-change-setup",
	}

	tx, err := registrystate.NewTransaction(s.state, "my-acc", "network")
	c.Assert(err, IsNil)

	t := s.state.NewTask("task", "")
	registrystate.SetTransaction(t, tx)

	ctx, err := hookstate.NewContext(t, s.state, hooksup, nil, "")
	c.Assert(err, IsNil)

	handler := hookstate.ChangeViewHandlerGenerator(ctx)
	s.state.Unlock()

	c.Assert(err, IsNil)
	err = handler.Done()
	c.Assert(err, IsNil)
}

func (s *hookHandlerSuite) TestViewChangeHookRejectsChanges(c *C) {
	s.state.Lock()
	hooksup := &hookstate.HookSetup{
		Snap: "test-snap",
		Hook: "view-change-setup",
	}

	tx, err := registrystate.NewTransaction(s.state, "my-acc", "network")
	c.Assert(err, IsNil)
	tx.Abort("my-snap", "don't like")

	t := s.state.NewTask("task", "")
	registrystate.SetTransaction(t, tx)

	ctx, err := hookstate.NewContext(t, s.state, hooksup, nil, "")
	c.Assert(err, IsNil)

	handler := hookstate.ChangeViewHandlerGenerator(ctx)
	s.state.Unlock()

	err = handler.Done()
	c.Assert(err, ErrorMatches, `cannot change registry my-acc/network: my-snap rejected changes: don't like`)
}

func (s *hookHandlerSuite) TestSaveViewHookOk(c *C) {
	s.state.Lock()
	hooksup := &hookstate.HookSetup{
		Snap: "test-snap",
		Hook: "view-change-setup",
	}

	tx, err := registrystate.NewTransaction(s.state, "my-acc", "network")
	c.Assert(err, IsNil)

	t := s.state.NewTask("task", "")
	registrystate.SetTransaction(t, tx)

	ctx, err := hookstate.NewContext(t, s.state, hooksup, nil, "")
	c.Assert(err, IsNil)

	handler := hookstate.SaveViewHandlerGenerator(ctx)
	s.state.Unlock()

	err = handler.Done()
	c.Assert(err, IsNil)
}

func (s *hookHandlerSuite) TestSaveViewHookErrorRollsBackSaves(c *C) {
	s.state.Lock()
	chg := s.state.NewChange("my-change", "")
	commitTask := s.state.NewTask("commit-registry-tx", "")
	chg.AddTask(commitTask)

	tx, err := registrystate.NewTransaction(s.state, "my-acc", "network")
	c.Assert(err, IsNil)
	registrystate.SetTransaction(commitTask, tx)

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

	handler := hookstate.SaveViewHandlerGenerator(ctx)
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
	tx, _, err = registrystate.GetStoredTransaction(secondTask)
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

	handler = hookstate.SaveViewHandlerGenerator(ctx)
	err = handler.Done()
	c.Assert(err, ErrorMatches, savingErr.Error())
}

func (s *hookHandlerSuite) TestSaveViewHookErrorHoldsTasks(c *C) {
	s.state.Lock()
	chg := s.state.NewChange("my-change", "")
	commitTask := s.state.NewTask("commit-registry-tx", "")
	chg.AddTask(commitTask)

	tx, err := registrystate.NewTransaction(s.state, "my-acc", "network")
	c.Assert(err, IsNil)
	registrystate.SetTransaction(commitTask, tx)

	err = tx.Set("foo", "bar")
	c.Assert(err, IsNil)

	// the first save-view hook will fail
	hooksup := &hookstate.HookSetup{
		Snap: "first-snap",
		Hook: "save-view-setup",
	}
	firstTask := s.state.NewTask("run-hook", "")
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
	secondTask := s.state.NewTask("run-hook", "")
	chg.AddTask(secondTask)
	secondTask.WaitFor(firstTask)
	secondTask.SetStatus(state.DoStatus)
	secondTask.Set("hook-setup", hooksup)

	ctx, err := hookstate.NewContext(firstTask, s.state, hooksup, nil, "")
	c.Assert(err, IsNil)

	handler := hookstate.SaveViewHandlerGenerator(ctx)

	s.state.Unlock()
	ignore, err := handler.Error(errors.New("failed to save"))
	c.Assert(err, IsNil)
	c.Assert(ignore, Equals, true)
	s.state.Lock()

	halts := firstTask.HaltTasks()
	c.Assert(halts, HasLen, 2)
	// the save-view hook for the second snap is held
	nextHook := halts[0]
	c.Assert(nextHook.Kind(), Equals, "run-hook")
	c.Assert(nextHook.Status(), Equals, state.HoldStatus)
	err = nextHook.Get("hook-setup", &hooksup)
	c.Assert(err, IsNil)
	c.Assert(hooksup.Hook, Equals, "save-view-setup")
	c.Assert(hooksup.Snap, Equals, "second-snap")

	// the rollback task for the failed hook
	rollbackTask := halts[1]
	c.Assert(rollbackTask.Kind(), Equals, "run-hook")
	c.Assert(rollbackTask.Status(), Equals, state.DoStatus)
	err = rollbackTask.Get("hook-setup", &hooksup)
	c.Assert(err, IsNil)
	c.Assert(hooksup.Hook, Equals, "save-view-setup")
	c.Assert(hooksup.Snap, Equals, "first-snap")
}
