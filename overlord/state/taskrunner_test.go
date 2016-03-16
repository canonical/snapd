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

package state_test

import (
	"errors"
	"sync"

	. "gopkg.in/check.v1"

	"github.com/ubuntu-core/snappy/overlord/state"
)

type taskRunnerSuite struct{}

var _ = Suite(&taskRunnerSuite{})

func (ts *taskRunnerSuite) TestAddHandler(c *C) {
	r := state.NewTaskRunner(nil)
	fn := func(task *state.Task) error {
		return nil
	}
	r.AddHandler("download", fn)

	c.Assert(r.Handlers(), HasLen, 1)
}

func (ts *taskRunnerSuite) TestEnsureTrivial(c *C) {
	// we need state
	st := state.New(nil)

	// setup the download handler
	taskCompleted := sync.WaitGroup{}
	r := state.NewTaskRunner(st)
	fn := func(task *state.Task) error {
		task.State().Lock()
		defer task.State().Unlock()
		c.Check(task.Status(), Equals, state.RunningStatus)
		taskCompleted.Done()
		return nil
	}
	r.AddHandler("download", fn)

	// add a download task to the state tracker
	st.Lock()
	chg := st.NewChange("install", "...")
	chg.NewTask("download", "1...")
	taskCompleted.Add(1)
	st.Unlock()

	defer r.Stop()

	// ensure just kicks the go routine off
	r.Ensure()
	taskCompleted.Wait()
}

func (ts *taskRunnerSuite) TestEnsureComplex(c *C) {
	// we need state
	st := state.New(nil)

	// setup handlers
	r := state.NewTaskRunner(st)

	var ordering []string
	fn := func(task *state.Task) error {
		task.State().Lock()
		defer task.State().Unlock()
		c.Check(task.Status(), Equals, state.RunningStatus)
		ordering = append(ordering, task.Kind())
		return nil
	}
	r.AddHandler("download", fn)
	r.AddHandler("unpack", fn)
	r.AddHandler("configure", fn)

	defer r.Stop()

	// run in a loop to ensure ordering is correct by pure chance
	for i := 0; i < 100; i++ {
		ordering = []string{}

		st.Lock()
		chg := st.NewChange("mock-install", "...")

		// create sub-tasks
		tDl := chg.NewTask("download", "1...")
		tUnp := chg.NewTask("unpack", "2...")
		tUnp.WaitFor(tDl)
		tConf := chg.NewTask("configure", "3...")
		tConf.WaitFor(tUnp)
		st.Unlock()

		for len(ordering) < 3 {
			// ensure just kicks the go routine off
			r.Ensure()
			// wait for them to finish
			r.Wait()
		}

		c.Assert(ordering, DeepEquals, []string{"download", "unpack", "configure"})
	}
}

func (ts *taskRunnerSuite) TestErrorIsFinal(c *C) {
	// we need state
	st := state.New(nil)

	invocations := 0

	// setup the download handler
	r := state.NewTaskRunner(st)
	fn := func(task *state.Task) error {
		invocations++
		return errors.New("boom")
	}
	r.AddHandler("download", fn)

	// add a download task to the state tracker
	st.Lock()
	chg := st.NewChange("install", "...")
	chg.NewTask("download", "1...")
	st.Unlock()

	defer r.Stop()

	// ensure just kicks the go routine off
	r.Ensure()
	r.Wait()
	r.Ensure()
	r.Wait()

	c.Check(invocations, Equals, 1)
}
