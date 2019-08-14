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

func (s *SnapSuite) TestDebugTasks(c *C) {
	dir := c.MkDir()
	stateFile := filepath.Join(dir, "test-state.json")
	c.Assert(ioutil.WriteFile(stateFile, stateJSON, 0644), IsNil)

	rest, err := main.Parser(main.Client()).ParseArgs([]string{"debug", "tasks", "--change-id=1", stateFile})
	c.Assert(err, IsNil)
	c.Assert(rest, DeepEquals, []string{})
	c.Check(s.Stdout(), Matches,
		"Lanes  ID   Status  Spawn                 Ready                 Kind             Summary\n"+
			"0      11   Done    0001-01-01T00:00:00Z  0001-01-01T00:00:00Z  download-snap    Download snap a from channel edge\n"+
			"0      12   Do      0001-01-01T00:00:00Z  0001-01-01T00:00:00Z  some-other-task  \n")
	c.Check(s.Stderr(), Equals, "")
}

func (s *SnapSuite) TestDebugTasksMissingState(c *C) {
	_, err := main.Parser(main.Client()).ParseArgs([]string{"debug", "tasks", "--change-id=1", "/missing-state.json"})
	c.Check(err, ErrorMatches, "cannot read the state file: open /missing-state.json: no such file or directory")
}

func (s *SnapSuite) TestDebugTasksMissingChangeID(c *C) {
	_, err := main.Parser(main.Client()).ParseArgs([]string{"debug", "tasks", "state.json"})
	c.Check(err, ErrorMatches, "the required flag `--change-id' was not specified")
}
