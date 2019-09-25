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
	apparmor_sandbox "github.com/snapcore/snapd/sandbox/apparmor"
	"github.com/snapcore/snapd/testutil"
)

func Test(t *testing.T) {
	TestingT(t)
}

type backendsSuite struct{}

var _ = Suite(&backendsSuite{})

func (s *backendsSuite) TestIsAppArmorEnabled(c *C) {
	for _, level := range []apparmor_sandbox.AppArmorLevelType{apparmor_sandbox.NoAppArmor, apparmor_sandbox.UnusableAppArmor, apparmor_sandbox.PartialAppArmor, apparmor_sandbox.FullAppArmor} {
		restore := apparmor_sandbox.MockLevel(level)
		defer restore()

		all := backends.Backends()
		names := make([]string, len(all))
		for i, backend := range all {
			names[i] = string(backend.Name())
		}
		switch level {
		case apparmor_sandbox.NoAppArmor, apparmor_sandbox.UnusableAppArmor:
			c.Assert(names, Not(testutil.Contains), "apparmor")
		case apparmor_sandbox.PartialAppArmor, apparmor_sandbox.FullAppArmor:
			c.Assert(names, testutil.Contains, "apparmor")
		}

	}
}

func (s *backendsSuite) TestEssentialOrdering(c *C) {
	restore := apparmor_sandbox.MockLevel(apparmor_sandbox.FullAppArmor)
	defer restore()

	all := backends.Backends()
	aaIndex := -1
	sdIndex := -1
	for i, backend := range all {
		switch backend.Name() {
		case "apparmor":
			aaIndex = i
		case "systemd":
			sdIndex = i
		}
	}
	c.Assert(aaIndex, testutil.IntNotEqual, -1)
	c.Assert(sdIndex, testutil.IntNotEqual, -1)
	c.Assert(sdIndex, testutil.IntLessThan, aaIndex)
}
