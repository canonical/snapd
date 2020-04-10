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

package naming_test

import (
	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/snap/naming"
)

type wellKnownSuite struct{}

var _ = Suite(&wellKnownSuite{})

func (s wellKnownSuite) TestWellKnownSnapID(c *C) {
	c.Check(naming.WellKnownSnapID("foo"), Equals, "")

	c.Check(naming.WellKnownSnapID("snapd"), Equals, "PMrrV4ml8uWuEUDBT8dSGnKUYbevVhc4")

	c.Check(naming.WellKnownSnapID("core"), Equals, "99T7MUlRhtI3U0QFgl5mXXESAiSwt776")
	c.Check(naming.WellKnownSnapID("core18"), Equals, "CSO04Jhav2yK0uz97cr0ipQRyqg0qQL6")
	c.Check(naming.WellKnownSnapID("core20"), Equals, "DLqre5XGLbDqg9jPtiAhRRjDuPVa5X1q")
}

func (s wellKnownSuite) TestWellKnownSnapIDStaging(c *C) {
	defer naming.UseStagingIDs(true)()

	c.Check(naming.WellKnownSnapID("baz"), Equals, "")

	c.Check(naming.WellKnownSnapID("snapd"), Equals, "Z44rtQD1v4r1LXGPCDZAJO3AOw1EDGqy")

	c.Check(naming.WellKnownSnapID("core"), Equals, "xMNMpEm0COPZy7jq9YRwWVLCD9q5peow")
	c.Check(naming.WellKnownSnapID("core18"), Equals, "NhSvwckvNdvgdiVGlsO1vYmi3FPdTZ9U")
	// XXX no core20 uploaded to staging yet
	c.Check(naming.WellKnownSnapID("core20"), Equals, "")
}
