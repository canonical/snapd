// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2017 Canonical Ltd
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

package cmdstate_test

import (
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"gopkg.in/check.v1"

	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/overlord"
	"github.com/snapcore/snapd/overlord/cmdstate"
	"github.com/snapcore/snapd/overlord/state"
)

// hook up gocheck to testing
func TestCommand(t *testing.T) { check.TestingT(t) }

type cmdSuite struct {
	rootdir string
	state   *state.State
	manager overlord.StateManager
	restore func()
}

var _ = check.Suite(&cmdSuite{})

type statr interface {
	Status() state.Status
}

func (s *cmdSuite) waitfor(thing statr) {
	s.state.Unlock()
	for i := 0; i < 5; i++ {
		s.manager.Ensure()
		s.manager.Wait()
		s.state.Lock()
		if thing.Status().Ready() {
			return
		}
		s.state.Unlock()
	}
	s.state.Lock()
}

func (s *cmdSuite) SetUpTest(c *check.C) {
	d := c.MkDir()
	dirs.SetRootDir(d)
	s.rootdir = d
	s.state = state.New(nil)
	s.manager = cmdstate.Manager(s.state)
	s.restore = cmdstate.MockExecTimeout(time.Second / 10)
}

func (s *cmdSuite) TearDownTest(c *check.C) {
	s.restore()
}

func (s *cmdSuite) TestKnownTaskKinds(c *check.C) {
	kinds := s.manager.KnownTaskKinds()
	sort.Strings(kinds)
	c.Assert(kinds, check.DeepEquals, []string{"exec-command"})
}

func (s *cmdSuite) TestExecTask(c *check.C) {
	s.state.Lock()
	defer s.state.Unlock()
	argvIn := []string{"/bin/echo", "hello"}
	tasks := cmdstate.Exec(s.state, "this is the summary", argvIn).Tasks()
	c.Assert(tasks, check.HasLen, 1)
	task := tasks[0]
	c.Check(task.Kind(), check.Equals, "exec-command")

	var argvOut []string
	c.Check(task.Get("argv", &argvOut), check.IsNil)
	c.Check(argvOut, check.DeepEquals, argvIn)
}

func (s *cmdSuite) TestExecHappy(c *check.C) {
	s.state.Lock()
	defer s.state.Unlock()

	fn := filepath.Join(s.rootdir, "flag")
	ts := cmdstate.Exec(s.state, "Doing the thing", []string{"touch", fn})
	chg := s.state.NewChange("do-the-thing", "Doing the thing")
	chg.AddAll(ts)

	s.waitfor(chg)

	c.Check(osutil.FileExists(fn), check.Equals, true)
	c.Check(chg.Status(), check.Equals, state.DoneStatus)
}

func (s *cmdSuite) TestExecSad(c *check.C) {
	s.state.Lock()
	defer s.state.Unlock()

	ts := cmdstate.Exec(s.state, "Doing the thing", []string{"sh", "-c", "echo hello; false"})
	chg := s.state.NewChange("do-the-thing", "Doing the thing")
	chg.AddAll(ts)

	s.waitfor(chg)

	c.Check(chg.Status(), check.Equals, state.ErrorStatus)
}

func (s *cmdSuite) TestExecAbort(c *check.C) {
	s.state.Lock()
	defer s.state.Unlock()

	ts := cmdstate.Exec(s.state, "Doing the thing", []string{"sleep", "1h"})
	chg := s.state.NewChange("do-the-thing", "Doing the thing")
	chg.AddAll(ts)

	s.state.Unlock()
	s.manager.Ensure()
	s.state.Lock()

	c.Assert(chg.Status(), check.Equals, state.DoingStatus)

	chg.Abort()

	s.waitfor(chg)

	c.Check(chg.Status(), check.Equals, state.ErrorStatus)
	c.Check(strings.Join(chg.Tasks()[0].Log(), "\n"), check.Matches, `(?s).*ERROR aborted`)
}

func (s *cmdSuite) TestExecStop(c *check.C) {
	s.state.Lock()
	defer s.state.Unlock()

	ts := cmdstate.Exec(s.state, "Doing the thing", []string{"sleep", "1h"})
	chg := s.state.NewChange("do-the-thing", "Doing the thing")
	chg.AddAll(ts)

	c.Assert(chg.Status(), check.Equals, state.DoStatus)

	s.state.Unlock()
	s.manager.Stop()
	s.state.Lock()

	c.Check(chg.Status(), check.Equals, state.DoStatus)
	chg.Abort()
}

func (s *cmdSuite) TestExecTimesOut(c *check.C) {
	s.state.Lock()
	defer s.state.Unlock()

	ts := cmdstate.Exec(s.state, "Doing the thing", []string{"sleep", "1m"})
	chg := s.state.NewChange("do-the-thing", "Doing the thing")
	chg.AddAll(ts)

	s.waitfor(chg)

	c.Check(chg.Status(), check.Equals, state.ErrorStatus)
	c.Check(strings.Join(chg.Tasks()[0].Log(), "\n"), check.Matches, `(?s).*ERROR exceeded maximum runtime.*`)
}
