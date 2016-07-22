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
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/snap/snaptest"
)

type gadgetYamlTestSuite struct {
}

var _ = Suite(&gadgetYamlTestSuite{})

var mockGadgetSnapYaml = `
name: canonical-pc
type: gadget
`

var mockGadgetYaml = []byte(`
bootloader: grub
`)

func (s *gadgetYamlTestSuite) SetUpTest(c *C) {
	dirs.SetRootDir(c.MkDir())
}

func (s *gadgetYamlTestSuite) TearDownTest(c *C) {
	dirs.SetRootDir("/")
}

func (s *gadgetYamlTestSuite) TestReadGadgetYamlMissing(c *C) {
	fn := snaptest.MockSnap(c, mockGadgetSnapYaml, &snap.SideInfo{Revision: snap.R(42)})
	_, err := snap.ReadGadgetInfo(fn)
	c.Assert(err, ErrorMatches, ".*meta/gadget.yaml: no such file or directory")
}

func (s *gadgetYamlTestSuite) TestReadGadgetYamlValid(c *C) {
	info := snaptest.MockSnap(c, mockGadgetSnapYaml, &snap.SideInfo{Revision: snap.R(42)})
	err := ioutil.WriteFile(filepath.Join(info.MountDir(), "meta", "gadget.yaml"), mockGadgetYaml, 0644)
	c.Assert(err, IsNil)

	ginfo, err := snap.ReadGadgetInfo(info)
	c.Assert(err, IsNil)
	c.Assert(ginfo, DeepEquals, &snap.GadgetInfo{Bootloader: "grub"})
}
