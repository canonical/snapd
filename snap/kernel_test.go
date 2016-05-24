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

type KernelYamlTestSuite struct {
}

var _ = Suite(&KernelYamlTestSuite{})

var mockKernelYaml = `name: canonical-pc-linux
type: kernel`

func (s *KernelYamlTestSuite) SetUpTest(c *C) {
	dirs.SetRootDir(c.MkDir())
}

func (s *KernelYamlTestSuite) TearDownTest(c *C) {
	dirs.SetRootDir("/")
}

func (s *KernelYamlTestSuite) TestReadKernelYamlMissing(c *C) {
	info := snaptest.MockSnap(c, mockKernelYaml, &snap.SideInfo{Revision: snap.R(42)})
	_, err := snap.ReadKernelInfo(info)
	c.Assert(err, ErrorMatches, ".*meta/kernel.yaml: no such file or directory")
}

func (s *KernelYamlTestSuite) TestReadKernelYamlValid(c *C) {
	info := snaptest.MockSnap(c, mockKernelYaml, &snap.SideInfo{Revision: snap.R(42)})
	err := ioutil.WriteFile(filepath.Join(info.MountDir(), "meta", "kernel.yaml"), []byte(`version: 4.2`), 0644)
	c.Assert(err, IsNil)

	kinfo, err := snap.ReadKernelInfo(info)
	c.Assert(err, IsNil)
	c.Assert(kinfo, DeepEquals, &snap.KernelInfo{Version: "4.2"})
}
