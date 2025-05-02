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

	"github.com/snapcore/snapd/interfaces"
	"github.com/snapcore/snapd/interfaces/backends"
	apparmor_sandbox "github.com/snapcore/snapd/sandbox/apparmor"
	"github.com/snapcore/snapd/snapdenv"
	"github.com/snapcore/snapd/testutil"
)

func Test(t *testing.T) {
	TestingT(t)
}

type backendsSuite struct{}

var _ = Suite(&backendsSuite{})

func (s *backendsSuite) TestIsAppArmorEnabled(c *C) {
	for _, level := range []apparmor_sandbox.LevelType{apparmor_sandbox.Unsupported, apparmor_sandbox.Unusable, apparmor_sandbox.Partial, apparmor_sandbox.Full} {
		restore := apparmor_sandbox.MockLevel(level)
		defer restore()

		switch level {
		case apparmor_sandbox.Unsupported, apparmor_sandbox.Unusable:
			c.Assert(backendNames(backends.All()), Not(testutil.Contains), "apparmor")
		case apparmor_sandbox.Partial, apparmor_sandbox.Full:
			c.Assert(backendNames(backends.All()), testutil.Contains, "apparmor")
		}
	}
}

func (s *backendsSuite) TestEssentialOrdering(c *C) {
	restore := apparmor_sandbox.MockLevel(apparmor_sandbox.Full)
	defer restore()

	all := backends.All()
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

func (s *backendsSuite) TestUdevInContainers(c *C) {
	cmd := testutil.MockCommand(c, "systemd-detect-virt", `
	for arg in "$@"; do
		if [ "$arg" = --container ]; then
			exit 0
		fi
	done

	exit 1
	`)

	defer cmd.Restore()
	c.Assert(backendNames(backends.All()), Not(testutil.Contains), "udev")
}

func (s *backendsSuite) TestUdevPreseedingInContainers(c *C) {
	cmd := testutil.MockCommand(c, "systemd-detect-virt", `
	for arg in "$@"; do
		if [ "$arg" = --container ]; then
			exit 0
		fi
	done

	exit 1
	`)
	restore := snapdenv.MockPreseeding(true)
	defer restore()

	defer cmd.Restore()
	c.Assert(backendNames(backends.All()), testutil.Contains, "udev")
}

func (s *backendsSuite) TestUdevNotInContainers(c *C) {
	cmd := testutil.MockCommand(c, "systemd-detect-virt", `
	for arg in "$@"; do
		if [ "$arg" = --container ]; then
			exit 1
		fi
	done

	exit 0
	`)

	defer cmd.Restore()
	c.Assert(backendNames(backends.All()), testutil.Contains, "udev")
}

func backendNames(bs []interfaces.SecurityBackend) (names []string) {
	names = make([]string, len(bs))
	for i, backend := range bs {
		names[i] = string(backend.Name())
	}

	return names
}
