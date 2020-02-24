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
	. "gopkg.in/check.v1"

	main "github.com/snapcore/snapd/cmd/snap-recovery-chooser"
)

type menuSuite struct{}

var _ = Suite(&menuSuite{})

func (s *menuSuite) TestResolve(c *C) {
	m := &main.Menu{
		Description: "Start into a previous version:",
		Entries: []main.Entry{
			{
				ID:   "something",
				Text: "else",
			}, {
				ID:   "nested",
				Text: "nested menu",
				Submenu: &main.Menu{
					Entries: []main.Entry{
						{
							ID:   "no-nested-prefix",
							Text: "nested this",
						}, {
							ID:   "nested/has-nested-prefix",
							Text: "nested that",
						},
					},
				},
			},
		},
	}

	resolved := main.ResolveMenu(m)
	expected := &main.Menu{
		Description: "Start into a previous version:",
		Entries: []main.Entry{
			{
				ID:   "something",
				Text: "else",
			}, {
				ID:   "nested",
				Text: "nested menu",
				Submenu: &main.Menu{
					Entries: []main.Entry{
						{
							ID:   "nested/no-nested-prefix",
							Text: "nested this",
						}, {
							ID:   "nested/has-nested-prefix",
							Text: "nested that",
						},
					},
				},
			},
		},
	}
	c.Assert(resolved, DeepEquals, expected)
}
