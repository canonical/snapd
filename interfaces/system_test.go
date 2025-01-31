// -*- Mote: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) Canonical Ltd
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

package interfaces_test

import (
	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/interfaces"
)

type SystemSuite struct{}

var _ = Suite(&SystemSuite{})

func (s *SystemSuite) TestIsTheSystemSnap(c *C) {
	for _, name := range []string{"", "snapd", "core"} {
		c.Check(interfaces.IsTheSystemSnap(name), Equals, true)
	}
	for _, name := range []string{"other", "blah", "core24"} {
		c.Check(interfaces.IsTheSystemSnap(name), Equals, false)
	}
}
