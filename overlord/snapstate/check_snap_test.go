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

package snapstate_test

import (
	"fmt"

	. "gopkg.in/check.v1"

	"github.com/ubuntu-core/snappy/arch"
	"github.com/ubuntu-core/snappy/snap"
	"github.com/ubuntu-core/snappy/snap/squashfs"

	"github.com/ubuntu-core/snappy/overlord/snapstate"
)

type checkSnapSuite struct{}

var _ = Suite(&checkSnapSuite{})

func (s *checkSnapSuite) TestOpenSnapFile(c *C) {
	const yaml = `name: hello
version: 1.0
apps:
 bin:
   command: bin
`

	snapPath := makeTestSnap(c, yaml)
	info, snapf, err := snapstate.OpenSnapFileImpl(snapPath, nil)
	c.Assert(err, IsNil)

	c.Assert(snapf, FitsTypeOf, &squashfs.Snap{})
	c.Check(info.Name(), Equals, "hello")
}

func (s *checkSnapSuite) TestOpenSnapFilebSideInfo(c *C) {
	const yaml = `name: foo
apps:
 bar:
  command: bin/bar
plugs:
  plug:
slots:
 slot:
`

	snapPath := makeTestSnap(c, yaml)
	si := snap.SideInfo{OfficialName: "blessed", Revision: 42}
	info, _, err := snapstate.OpenSnapFileImpl(snapPath, &si)
	c.Assert(err, IsNil)

	// check side info
	c.Check(info.Name(), Equals, "blessed")
	c.Check(info.Revision, Equals, 42)

	c.Check(info.SideInfo, DeepEquals, si)

	// ensure that all leaf objects link back to the same snap.Info
	// and not to some copy.
	// (we had a bug around this)
	c.Check(info.Apps["bar"].Snap, Equals, info)
	c.Check(info.Plugs["plug"].Snap, Equals, info)
	c.Check(info.Slots["slot"].Snap, Equals, info)

}

func (s *checkSnapSuite) TestCheckSnapErrorOnUnsupportedArchitecture(c *C) {
	const yaml = `name: hello
version: 1.10
architectures:
    - yadayada
    - blahblah
`
	info, err := snap.InfoFromSnapYaml([]byte(yaml))
	c.Assert(err, IsNil)

	var openSnapFile = func(path string, si *snap.SideInfo) (*snap.Info, snap.File, error) {
		c.Check(path, Equals, "snap-path")
		c.Check(si, IsNil)
		return info, nil, nil
	}
	restore := snapstate.MockOpenSnapFile(openSnapFile)
	defer restore()

	err = snapstate.CheckSnap("snap-path", nil, 0)

	errorMsg := fmt.Sprintf(`snap "hello" supported architectures (yadayada, blahblah) are incompatible with this system (%s)`, arch.UbuntuArchitecture())
	c.Assert(err.Error(), Equals, errorMsg)
}
