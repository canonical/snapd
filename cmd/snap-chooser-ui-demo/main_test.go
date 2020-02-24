// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2020 Canonical Ltd
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
	"testing"

	. "gopkg.in/check.v1"

	main "github.com/snapcore/snapd/cmd/snap-chooser-ui-demo"
	"github.com/snapcore/snapd/testutil"
)

// Hook up check.v1 into the "go test" runner
func Test(t *testing.T) { TestingT(t) }

type cmdSuite struct {
	testutil.BaseTest
}

var _ = Suite(&cmdSuite{})

func (s *cmdSuite) TestController(c *C) {
	m := &main.Menu{
		Description: "Start into a previous version:",
		Entries: []main.Entry{
			{
				ID:   "something 1",
				Text: "else",
			}, {
				ID:   "nested 2",
				Text: "nested menu",
				Submenu: &main.Menu{
					Entries: []main.Entry{
						{
							ID:   "nested/something",
							Text: "nested else",
						},
					},
				},
			}, {
				ID:   "something 3",
				Text: "even more",
			},
		},
	}

	st := main.NewState(m)
	ctrl := main.NewController(st)
	curr, idx := st.CurrentMenuAndEntry()
	c.Check(curr, DeepEquals, m)
	c.Check(idx, DeepEquals, 0)

	ctrl.Up()
	curr, idx = st.CurrentMenuAndEntry()
	c.Check(curr, DeepEquals, m)
	c.Check(idx, DeepEquals, 0)

	// again to make sure there's no entry indexing problem
	ctrl.Up()
	curr, idx = st.CurrentMenuAndEntry()
	c.Check(curr, DeepEquals, m)
	c.Check(idx, DeepEquals, 0)

	ctrl.Down()
	curr, idx = st.CurrentMenuAndEntry()
	c.Check(curr, DeepEquals, m)
	c.Check(idx, DeepEquals, 1)

	// enter submenu
	ctrl.Enter()
	curr, idx = st.CurrentMenuAndEntry()
	c.Check(curr, DeepEquals, m.Entries[1].Submenu)
	c.Check(idx, DeepEquals, main.EntryGoBack)

	// down to first option
	ctrl.Down()
	curr, idx = st.CurrentMenuAndEntry()
	c.Check(curr, DeepEquals, m.Entries[1].Submenu)
	c.Check(idx, DeepEquals, 0)

	// final entry
	act := ctrl.Enter()
	c.Check(act, Equals, "nested/something")

	// move back up to implicit 'go back' entry
	ctrl.Up()
	curr, idx = st.CurrentMenuAndEntry()
	c.Check(curr, DeepEquals, m.Entries[1].Submenu)
	c.Check(idx, DeepEquals, main.EntryGoBack)

	ctrl.Enter()
	curr, idx = st.CurrentMenuAndEntry()
	c.Check(curr, DeepEquals, m)
	c.Check(idx, DeepEquals, 1)

	// down twice to make sure there's no entry indexing problem
	ctrl.Down()
	ctrl.Down()
	curr, idx = st.CurrentMenuAndEntry()
	c.Check(curr, DeepEquals, m)
	c.Check(idx, DeepEquals, 2)

	// final entry
	act = ctrl.Enter()
	c.Check(act, Equals, "something 3")

	// back at the beginning
	ctrl.Up()
	ctrl.Up()
	ctrl.Up()
	curr, idx = st.CurrentMenuAndEntry()
	c.Check(curr, DeepEquals, m)
	c.Check(idx, DeepEquals, 0)
}
