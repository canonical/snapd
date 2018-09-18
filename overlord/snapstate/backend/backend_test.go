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
	"github.com/snapcore/snapd/testutil"
)

func TestBackend(t *testing.T) { TestingT(t) }

func makeTestSnap(c *C, snapYamlContent string) string {
	info := snaptest.MockInfo(c, snapYamlContent, nil)
	var files [][]string
	for _, app := range info.Apps {
		// files is a list of (filename, content)
		files = append(files, []string{app.Command, ""})
	}
	return snaptest.MakeTestSnapWithFiles(c, snapYamlContent, files)
}

type backendSuite struct {
	testutil.BaseTest
}

var _ = Suite(&backendSuite{})

func (s *backendSuite) SetUpTest(c *C) {
	s.BaseTest.SetUpTest(c)
	s.BaseTest.AddCleanup(snap.MockSanitizePlugsSlots(func(snapInfo *snap.Info) {}))
}

func (s *backendSuite) TearDownTest(c *C) {
	s.BaseTest.TearDownTest(c)
}

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
	c.Check(info.InstanceName(), Equals, "hello")
}

func (s *backendSuite) TestOpenSnapFilebSideInfo(c *C) {
	const yaml = `name: foo
version: 0
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
	c.Check(info.InstanceName(), Equals, "blessed")
	c.Check(info.Revision, Equals, snap.R(42))

	c.Check(info.SideInfo, DeepEquals, si)

	// ensure that all leaf objects link back to the same snap.Info
	// and not to some copy.
	// (we had a bug around this)
	c.Check(info.Apps["bar"].Snap, Equals, info)
	c.Check(info.Plugs["plug"].Snap, Equals, info)
	c.Check(info.Slots["slot"].Snap, Equals, info)

}
