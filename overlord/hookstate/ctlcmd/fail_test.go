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

package ctlcmd_test

import (
	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/i18n"
	"github.com/snapcore/snapd/overlord/confdbstate"
	"github.com/snapcore/snapd/overlord/hookstate"
	"github.com/snapcore/snapd/overlord/hookstate/ctlcmd"
	"github.com/snapcore/snapd/snap"
)

func (s *confdbSuite) TestFailAbortsConfdbTransaction(c *C) {
	s.state.Lock()
	chg := s.state.NewChange("test", "")
	commitTask := s.state.NewTask("commit-confdb-tx", "")
	chg.AddTask(commitTask)
	tx, err := confdbstate.NewTransaction(s.state, "my-acc", "my-reg")
	c.Assert(err, IsNil)

	err = tx.Set(parsePath(c, "foo"), "bar")
	c.Assert(err, IsNil)

	commitTask.Set("confdb-transaction", tx)

	task := s.state.NewTask("run-hook", "")
	chg.AddTask(task)
	setup := &hookstate.HookSetup{Snap: "test-snap", Revision: snap.R(1), Hook: "change-view-plug"}
	task.Set("tx-task", commitTask.ID())
	s.state.Unlock()

	mockContext, err := hookstate.NewContext(task, s.state, setup, s.mockHandler, "")
	c.Assert(err, IsNil)

	stdout, stderr, err := ctlcmd.Run(mockContext, []string{"fail", "don't like changes"}, 0)
	c.Assert(err, IsNil)
	c.Check(stdout, IsNil)
	c.Check(stderr, IsNil)

	tx = nil
	s.state.Lock()
	err = commitTask.Get("confdb-transaction", &tx)
	s.state.Unlock()
	c.Assert(err, IsNil)

	snap, reason := tx.AbortInfo()
	c.Assert(reason, Equals, "don't like changes")
	c.Assert(snap, Equals, "test-snap")
}

func (s *confdbSuite) TestFailErrors(c *C) {
	s.state.Lock()
	s.setConfdbFlag(false, c)
	s.state.Unlock()

	stdout, stderr, err := ctlcmd.Run(s.mockContext, []string{"fail", "reason"}, 0)
	c.Assert(err, ErrorMatches, i18n.G(`"confdb" feature flag is disabled: set 'experimental.confdb' to true`))
	c.Check(stdout, IsNil)
	c.Check(stderr, IsNil)

	s.state.Lock()
	s.setConfdbFlag(true, c)
	s.state.Unlock()

	stdout, stderr, err = ctlcmd.Run(s.mockContext, []string{"fail"}, 0)
	c.Assert(err, ErrorMatches, i18n.G("the required argument `:<rejection-reason>` was not provided"))
	c.Check(stdout, IsNil)
	c.Check(stderr, IsNil)

	// ephemeral context - no hook
	setup := &hookstate.HookSetup{Snap: "test-snap", Revision: snap.R(1)}
	s.mockContext, err = hookstate.NewContext(nil, s.state, setup, s.mockHandler, "")
	c.Assert(err, IsNil)

	stdout, stderr, err = ctlcmd.Run(s.mockContext, []string{"fail", "reason"}, 0)
	c.Assert(err, ErrorMatches, i18n.G(`cannot use "snapctl fail" outside of a "change-view" hook`))
	c.Check(stdout, IsNil)
	c.Check(stderr, IsNil)

	// unexpected hook
	s.state.Lock()
	task := s.state.NewTask("run-hook", "my test task")
	setup = &hookstate.HookSetup{Snap: "test-snap", Revision: snap.R(1), Hook: "other-hook"}
	s.state.Unlock()

	s.mockContext, err = hookstate.NewContext(task, s.state, setup, s.mockHandler, "")
	c.Assert(err, IsNil)

	stdout, stderr, err = ctlcmd.Run(s.mockContext, []string{"fail", "reason"}, 0)
	c.Assert(err, ErrorMatches, i18n.G(`cannot use "snapctl fail" outside of a "change-view" hook`))
	c.Check(stdout, IsNil)
	c.Check(stderr, IsNil)

	setup = &hookstate.HookSetup{Snap: "test-snap", Revision: snap.R(1), Hook: "change-view-plug"}
	s.mockContext, err = hookstate.NewContext(task, s.state, setup, s.mockHandler, "")
	c.Assert(err, IsNil)

	stdout, stderr, err = ctlcmd.Run(s.mockContext, []string{"fail", "reason"}, 0)
	// this shouldn't happen but check we handle it well anyway
	c.Assert(err, ErrorMatches, i18n.G("internal error: cannot get confdb transaction to fail: no state entry for key \"tx-task\""))
	c.Check(stdout, IsNil)
	c.Check(stderr, IsNil)
}
