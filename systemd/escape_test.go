// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2014-2015 Canonical Ltd
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

package systemd_test

import (
	. "gopkg.in/check.v1"

	. "github.com/ubuntu-core/snappy/systemd"
)

func (s *SystemdTestSuite) TestEscape(c *C) {
	c.Check(EscapeUnitNamePath("Hallöchen, Meister"), Equals, `Hall\xc3\xb6chen\x2c\x20Meister`)

	c.Check(EscapeUnitNamePath("/tmp//waldi/foobar/"), Equals, `tmp-waldi-foobar`)
	c.Check(EscapeUnitNamePath("/.foo/.bar"), Equals, `\x2efoo-.bar`)
	c.Check(EscapeUnitNamePath("////"), Equals, `-`)
	c.Check(EscapeUnitNamePath("."), Equals, `\x2e`)
	c.Check(EscapeUnitNamePath("/foo/bar-baz"), Equals, `foo-bar\x2dbaz`)
	c.Check(EscapeUnitNamePath("/foo/bar--baz"), Equals, `foo-bar\x2d\x2dbaz`)
}
