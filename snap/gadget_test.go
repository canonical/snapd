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

package snap_test

import (
	"io/ioutil"
	"path/filepath"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/gadget"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/snap/snaptest"
)

type gadgetYamlTestSuite struct{}

var _ = Suite(&gadgetYamlTestSuite{})

var mockGadgetSnapYaml = `
name: canonical-pc
type: gadget
`

func (s *gadgetYamlTestSuite) SetUpTest(c *C) {
	dirs.SetRootDir(c.MkDir())
}

func (s *gadgetYamlTestSuite) TearDownTest(c *C) {
	dirs.SetRootDir("/")
}

func (s *gadgetYamlTestSuite) TestReadGadgetNotAGadget(c *C) {
	info := snaptest.MockInfo(c, `
name: other
version: 0
`, &snap.SideInfo{Revision: snap.R(42)})
	_, err := snap.ReadGadgetInfo(info, false)
	c.Assert(err, ErrorMatches, "cannot read gadget snap details: not a gadget snap")
}

func (s *gadgetYamlTestSuite) TestReadGadgetYamlMissing(c *C) {
	info := snaptest.MockSnap(c, mockGadgetSnapYaml, &snap.SideInfo{Revision: snap.R(42)})
	_, err := snap.ReadGadgetInfo(info, false)
	c.Assert(err, ErrorMatches, ".*meta/gadget.yaml: no such file or directory")
}

func (s *gadgetYamlTestSuite) TestReadGadgetYamlOnClassicOptional(c *C) {
	info := snaptest.MockSnap(c, mockGadgetSnapYaml, &snap.SideInfo{Revision: snap.R(42)})
	gi, err := snap.ReadGadgetInfo(info, true)
	c.Assert(err, IsNil)
	c.Check(gi, NotNil)
}

func (s *gadgetYamlTestSuite) TestReadGadgetYamlOnClassicEmptyIsValid(c *C) {
	info := snaptest.MockSnap(c, mockGadgetSnapYaml, &snap.SideInfo{Revision: snap.R(42)})
	err := ioutil.WriteFile(filepath.Join(info.MountDir(), "meta", "gadget.yaml"), nil, 0644)
	c.Assert(err, IsNil)

	ginfo, err := snap.ReadGadgetInfo(info, true)
	c.Assert(err, IsNil)
	c.Assert(ginfo, DeepEquals, &gadget.Info{})
}
