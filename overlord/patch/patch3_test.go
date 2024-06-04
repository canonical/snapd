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

package patch_test

import (
	"os"
	"path/filepath"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/overlord/patch"
	"github.com/snapcore/snapd/overlord/state"
)

type patch3Suite struct{}

var _ = Suite(&patch3Suite{})

var statePatch3JSON = []byte(`
{
        "last-task-id": 999,
        "last-change-id": 99,

	"data": {
		"patch-level": 2
	},
        "changes": {
		"1": {
			"id": "1",
			"kind": "some-change",
			"summary": "summary-1",
			"status": 0,
			"task-ids": ["1"]
		}
        },
	"tasks": {
		"1": {
			"id": "1",
			"kind": "link-snap",
			"summary": "meep",
			"status": 2,
			"halt-tasks": [
				"7"
			],
			"change": "1"
		},
		"2": {
			"id": "2",
			"kind": "unlink-snap",
			"summary": "meep",
			"status": 2,
			"halt-tasks": [
				"3"
			],
			"change": "1"
		},
		"3": {
			"id": "3",
			"kind": "unrelated",
			"summary": "meep",
			"status": 4,
			"halt-tasks": [
				"3"
			],
			"change": "1"
		},
		"4": {
			"id": "4",
			"kind": "unlink-current-snap",
			"summary": "meep",
			"status": 2,
			"halt-tasks": [
				"3"
			],
			"change": "1"
		}
	}
}
`)

func (s *patch3Suite) SetUpTest(c *C) {
	dirs.SetRootDir(c.MkDir())

	err := os.MkdirAll(filepath.Dir(dirs.SnapStateFile), 0755)
	c.Assert(err, IsNil)
	err = os.WriteFile(dirs.SnapStateFile, statePatch3JSON, 0644)
	c.Assert(err, IsNil)
}

func (s *patch3Suite) TestPatch3(c *C) {
	restorer := patch.MockLevel(3, 1)
	defer restorer()

	r, err := os.Open(dirs.SnapStateFile)
	c.Assert(err, IsNil)
	defer r.Close()
	st, err := state.ReadState(nil, r)
	c.Assert(err, IsNil)

	// our mocks are correct
	st.Lock()
	c.Assert(st.Tasks(), HasLen, 4)
	st.Unlock()

	// go from patch level 2 -> 3
	err = patch.Apply(st)
	c.Assert(err, IsNil)

	st.Lock()
	defer st.Unlock()

	// we got two more tasks
	c.Assert(st.Tasks(), HasLen, 7)
	for _, t := range st.Tasks() {
		switch t.Kind() {
		case "start-snap-services":
			ht := t.WaitTasks()
			c.Check(ht, HasLen, 1)
			c.Check(ht[0].Kind(), Equals, "link-snap")
		case "unlink-snap":
			ht := t.WaitTasks()
			c.Check(ht, HasLen, 1)
			c.Check(ht[0].Kind(), Equals, "stop-snap-services")
		case "unlink-current-snap":
			ht := t.WaitTasks()
			c.Check(ht, HasLen, 1)
			c.Check(ht[0].Kind(), Equals, "stop-snap-services")
		}
	}
}
