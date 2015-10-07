// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2015 Canonical Ltd
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

package daemon

import (
	"gopkg.in/check.v1"
)

type uuidSuite struct{}

var _ = check.Suite(&uuidSuite{})

func (s *uuidSuite) TestUUID(c *check.C) {
	uuids := make([]UUID, 200)
	for i := range uuids {
		uuids[i] = UUID4()
	}

	for i := range uuids[:len(uuids)-2] {
		c.Check(uuids[i].String(), check.Matches, `^[[:xdigit:]]{8}-[[:xdigit:]]{4}-[[:xdigit:]]{4}-[[:xdigit:]]{4}-[[:xdigit:]]{12}`, check.Commentf("format of %d wrong: %s", i, uuids[i]))
		for j := range uuids[i+1:] {
			c.Check(uuids[i], check.Not(check.Equals), uuids[i+j+1], check.Commentf("%d == %d", i, i+j+1))
		}
	}
}
