/*
 * Copyright (C) 2017 Canonical Ltd
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

package backends_test

import (
	"testing"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/interfaces/backends"
	"github.com/snapcore/snapd/release"
	"github.com/snapcore/snapd/testutil"
)

func Test(t *testing.T) {
	TestingT(t)
}

type backendsSuite struct{}

var _ = Suite(&backendsSuite{})

func (s *backendsSuite) TestIsAppArmorEnabled(c *C) {
	for _, level := range []release.AppArmorLevelType{release.NoAppArmor, release.PartialAppArmor, release.FullAppArmor} {
		restore := release.MockAppArmorLevel(level)
		defer restore()

		all := backends.Backends()
		names := make([]string, len(all))
		for i, backend := range all {
			names[i] = string(backend.Name())
		}

		if level == release.NoAppArmor {
			c.Assert(names, Not(testutil.Contains), "apparmor")
		} else {
			c.Assert(names, testutil.Contains, "apparmor")
		}
	}
}
