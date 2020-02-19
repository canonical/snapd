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

	main "github.com/snapcore/snapd/cmd/snap-bootstrap"
)

func (s *cmdSuite) TestCheckChooser(c *C) {
	n := 0
	restore := main.MockInputwatchWaitKey(func() error {
		n++
		return nil
	})
	defer restore()

	rest, err := main.Parser.ParseArgs([]string{"check-chooser"})
	c.Assert(err, IsNil)
	c.Assert(rest, HasLen, 0)
	c.Assert(n, Equals, 1)
}
