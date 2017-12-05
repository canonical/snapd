// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2016 Canonical Ltd
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

package builtin

import (
	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/interfaces/udev"
	"github.com/snapcore/snapd/testutil"
)

type commonIfaceSuite struct{}

var _ = Suite(&commonIfaceSuite{})

func (s *commonIfaceSuite) TestUDevSpec(c *C) {
	plug, _ := MockConnectedPlug(c, `
name: consumer
apps:
  app-a:
    plugs: [common]
  app-b:
  app-c:
    plugs: [common]
`, nil, "common")
	slot, _ := MockConnectedSlot(c, `
name: producer
slots:
  common:
`, nil, "common")

	// common interface can define connected plug udev rules
	iface := &commonInterface{
		name:              "common",
		connectedPlugUDev: []string{`KERNEL=="foo"`},
	}
	spec := &udev.Specification{}
	c.Assert(spec.AddConnectedPlug(iface, plug, slot), IsNil)
	c.Assert(spec.Snippets(), DeepEquals, []string{
		`# common
KERNEL=="foo", TAG+="snap_consumer_app-a"`,
		// NOTE: app-b is unaffected as it doesn't have a plug reference.
		`# common
KERNEL=="foo", TAG+="snap_consumer_app-c"`,
	})

	// connected plug udev rules are optional
	iface = &commonInterface{
		name: "common",
	}
	spec = &udev.Specification{}
	c.Assert(spec.AddConnectedPlug(iface, plug, slot), IsNil)
	c.Assert(spec.Snippets(), HasLen, 0)
}

// MockEvalSymlinks replaces the path/filepath.EvalSymlinks function used inside the caps package.
func MockEvalSymlinks(test *testutil.BaseTest, fn func(string) (string, error)) {
	orig := evalSymlinks
	evalSymlinks = fn
	test.AddCleanup(func() {
		evalSymlinks = orig
	})
}
