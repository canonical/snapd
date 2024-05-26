// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2023 Canonical Ltd
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

package naming_test

import (
	. "gopkg.in/check.v1"

	"github.com/ddkwork/golibrary/mylog"
	"github.com/snapcore/snapd/snap/naming"
)

type CoreVersionTestSuite struct{}

var _ = Suite(&CoreVersionTestSuite{})

func (s *CoreVersionTestSuite) TestCoreVersion(c *C) {
	for _, tst := range []struct {
		name    string
		version int
	}{
		{"core", 16},
		{"core20", 20},
		{"core22", 22},
		{"core24", 24},
		{"core24-desktop", 24},
		{"core24-server", 24},
	} {
		v := mylog.Check2(naming.CoreVersion(tst.name))
		c.Check(err, IsNil)
		c.Check(v, Equals, tst.version)
	}
}

func (s *CoreVersionTestSuite) TestCoreOther(c *C) {
	for _, tst := range []string{"bare", "coreXX", "coreXX-desktop", "core24_desktop"} {
		_ := mylog.Check2(naming.CoreVersion(tst))
		c.Check(err, ErrorMatches, "not a core base")
	}
}
