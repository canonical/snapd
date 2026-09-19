// -*- Mode: Go; indent-tabs-mode: t -*-

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

package builtin_test

import (
	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/interfaces/builtin"
)

type driverLibsHelpersSuite struct{}

var _ = Suite(&driverLibsHelpersSuite{})

func (s *driverLibsHelpersSuite) TestDriverLibsSupported(c *C) {
	// Bases that are not new enough for the direct-connection model.
	for _, base := range []string{"", "bare", "core", "core18", "core20", "core22", "core24", "core22-desktop", "core24-desktop"} {
		c.Check(builtin.DriverLibsSupported(base), Equals, false, Commentf("base %q should be unsupported", base))
	}
	// Bases new enough (core26 and beyond) are supported.
	for _, base := range []string{"core26", "core28", "core26-desktop"} {
		c.Check(builtin.DriverLibsSupported(base), Equals, true, Commentf("base %q should be supported", base))
	}
}
