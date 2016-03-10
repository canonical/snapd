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

	// ensure just kicks the go routine off
	r.Ensure()
	taskCompleted.Wait()
}
