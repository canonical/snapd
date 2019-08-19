// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2019 Canonical Ltd
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

package main_test

import (
	"io/ioutil"
	"path/filepath"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/cmd/snap"
)

func (s *SnapSuite) TestDebugTask(c *C) {
	dir := c.MkDir()
	stateFile := filepath.Join(dir, "test-state.json")
	c.Assert(ioutil.WriteFile(stateFile, stateJSON, 0644), IsNil)

	rest, err := main.Parser(main.Client()).ParseArgs([]string{"debug", "task", "--task-id=31", stateFile})
	c.Assert(err, IsNil)
	c.Assert(rest, DeepEquals, []string{})
	c.Check(s.Stdout(), Matches, "id: 31\n"+
		"kind: prepare-snap\n"+
		"summary: Prepare snap c\n"+
		"status: Done\n"+
		"\n"+
		"log: |\n"+
		"  logline1\n"+
		"  logline2\n"+
		"\n"+
		"tasks waiting for 31:\n"+
		"  some-other-task \\(12\\)\n")
	c.Check(s.Stderr(), Equals, "")
}

func (s *SnapSuite) TestDebugTaskMissingState(c *C) {
	_, err := main.Parser(main.Client()).ParseArgs([]string{"debug", "task", "--task-id=1", "/missing-state.json"})
	c.Check(err, ErrorMatches, "cannot read the state file: open /missing-state.json: no such file or directory")
}

func (s *SnapSuite) TestDebugTaskMissingTaskID(c *C) {
	_, err := main.Parser(main.Client()).ParseArgs([]string{"debug", "task", "state.json"})
	c.Check(err, ErrorMatches, "the required flag `--task-id' was not specified")
}

func (s *SnapSuite) TestDebugTaskNoSuchTaskError(c *C) {
	dir := c.MkDir()
	stateFile := filepath.Join(dir, "test-state.json")
	c.Assert(ioutil.WriteFile(stateFile, stateJSON, 0644), IsNil)

	_, err := main.Parser(main.Client()).ParseArgs([]string{"debug", "task", "--task-id=99", stateFile})
	c.Check(err, ErrorMatches, "no such task: 99")
}
