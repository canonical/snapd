// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2014-2015 Canonical Ltd
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

package partition

import (
	"fmt"
	"io/ioutil"
	"os"
	"sort"

	"github.com/snapcore/snapd/dirs"

	. "gopkg.in/check.v1"
)

func mockGrubEditenvList(cmd ...string) (string, error) {
	mockGrubEditenvOutput := fmt.Sprintf("%s=regular", bootmodeVar)
	return mockGrubEditenvOutput, nil
}

func mockGrubFile(c *C, newPath string, mode os.FileMode) {
	err := ioutil.WriteFile(newPath, []byte(""), mode)
	c.Assert(err, IsNil)
}

func (s *PartitionTestSuite) makeFakeGrubEnv(c *C) {
	// these files just needs to exist
	g := &grub{}
	mockGrubFile(c, g.ConfigFile(), 0644)
	mockGrubFile(c, g.envFile(), 0644)
}

func (s *PartitionTestSuite) TestNewGrubNoGrubReturnsNil(c *C) {
	dirs.GlobalRootDir = "/something/not/there"

	g := newGrub()
	c.Assert(g, IsNil)
}

func (s *PartitionTestSuite) TestNewGrub(c *C) {
	s.makeFakeGrubEnv(c)

	g := newGrub()
	c.Assert(g, NotNil)
	c.Assert(g, FitsTypeOf, &grub{})
}

func (s *PartitionTestSuite) TestGetBootloaderWithGrub(c *C) {
	s.makeFakeGrubEnv(c)

	bootloader, err := FindBootloader()
	c.Assert(err, IsNil)
	c.Assert(bootloader, FitsTypeOf, &grub{})
}

func (s *PartitionTestSuite) TestGetBootVer(c *C) {
	s.makeFakeGrubEnv(c)
	runCommand = mockGrubEditenvList

	g := newGrub()
	v, err := g.GetBootVars(bootmodeVar)
	c.Assert(err, IsNil)
	c.Check(v, HasLen, 1)
	c.Check(v[bootmodeVar], Equals, "regular")
}

func (s *PartitionTestSuite) TestSetBootVer(c *C) {
	s.makeFakeGrubEnv(c)
	cmds := [][]string{}
	runCommand = func(cmd ...string) (string, error) {
		cmds = append(cmds, cmd)
		return "", nil
	}

	g := newGrub()
	err := g.SetBootVars(map[string]string{
		"k1": "v1",
		"k2": "v2",
	})
	c.Assert(err, IsNil)
	c.Check(cmds, HasLen, 1)
	c.Check(cmds[0][0:3], DeepEquals, []string{
		"/usr/bin/grub-editenv", g.(*grub).envFile(), "set",
	})
	// need to sort, its coming from a slice
	kwargs := cmds[0][3:]
	sort.Strings(kwargs)
	c.Check(kwargs, DeepEquals, []string{"k1=v1", "k2=v2"})
}
