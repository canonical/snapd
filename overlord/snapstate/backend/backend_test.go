// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2014-2016 Canonical Ltd
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

package backend_test

import (
	"testing"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/snap/snaptest"
	"github.com/snapcore/snapd/snap/squashfs"

	"github.com/snapcore/snapd/overlord/snapstate/backend"
)

func TestBackend(t *testing.T) { TestingT(t) }

func makeTestSnap(c *C, snapYamlContent string) string {
	return snaptest.MakeTestSnapWithFiles(c, snapYamlContent, nil)
}

type backendSuite struct{}

var _ = Suite(&backendSuite{})

func (s *backendSuite) TestOpenSnapFile(c *C) {
	const yaml = `name: hello
version: 1.0
apps:
 bin:
   command: bin
`

	snapPath := makeTestSnap(c, yaml)
	info, snapf, err := backend.OpenSnapFile(snapPath, nil)
	c.Assert(err, IsNil)

	c.Assert(snapf, FitsTypeOf, &squashfs.Snap{})
	c.Check(info.Name(), Equals, "hello")
}

func (s *backendSuite) TestOpenSnapFilebSideInfo(c *C) {
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
	si := snap.SideInfo{RealName: "blessed", Revision: snap.R(42)}
	info, _, err := backend.OpenSnapFile(snapPath, &si)
	c.Assert(err, IsNil)

	// check side info
	c.Check(info.Name(), Equals, "blessed")
	c.Check(info.Revision, Equals, snap.R(42))

	c.Check(info.SideInfo, DeepEquals, si)

	// ensure that all leaf objects link back to the same snap.Info
	// and not to some copy.
	// (we had a bug around this)
	c.Check(info.Apps["bar"].Snap, Equals, info)
	c.Check(info.Plugs["plug"].Snap, Equals, info)
	c.Check(info.Slots["slot"].Snap, Equals, info)

}
