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
	"os/exec"
	"path/filepath"

	"github.com/mvo5/goconfigparser"
	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/osutil"
)

// grubEditenvCmd finds the right grub{,2}-editenv command
func grubEditenvCmd() string {
	for _, exe := range []string{"grub2-editenv", "grub-editenv"} {
		if osutil.ExecutableExists(exe) {
			return exe
		}
	}
	return ""
}

func grubEnvPath() string {
	return filepath.Join(dirs.GlobalRootDir, "boot/grub/grubenv")
}

func grubEditenvSet(c *C, key, value string) {
	if grubEditenvCmd() == "" {
		c.Skip("grub{,2}-editenv is not available")
	}

	err := exec.Command(grubEditenvCmd(), grubEnvPath(), "set", fmt.Sprintf("%s=%s", key, value)).Run()
	c.Assert(err, IsNil)
}

func grubEditenvGet(c *C, key string) string {
	if grubEditenvCmd() == "" {
		c.Skip("grub{,2}-editenv is not available")
	}

	output, err := exec.Command(grubEditenvCmd(), grubEnvPath(), "list").CombinedOutput()
	c.Assert(err, IsNil)
	cfg := goconfigparser.New()
	cfg.AllowNoSectionHeader = true
	err = cfg.ReadString(string(output))
	c.Assert(err, IsNil)
	v, err := cfg.Get("", key)
	c.Assert(err, IsNil)
	return v
}

func (s *PartitionTestSuite) makeFakeGrubEnv(c *C) {
	g := &grub{}
	err := ioutil.WriteFile(g.ConfigFile(), nil, 0644)
	c.Assert(err, IsNil)
	grubEditenvSet(c, "k", "v")
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
	grubEditenvSet(c, bootmodeVar, "regular")

	g := newGrub()
	v, err := g.GetBootVars(bootmodeVar)
	c.Assert(err, IsNil)
	c.Check(v, HasLen, 1)
	c.Check(v[bootmodeVar], Equals, "regular")
}

func (s *PartitionTestSuite) TestSetBootVer(c *C) {
	s.makeFakeGrubEnv(c)

	g := newGrub()
	err := g.SetBootVars(map[string]string{
		"k1": "v1",
		"k2": "v2",
	})
	c.Assert(err, IsNil)

	c.Check(grubEditenvGet(c, "k1"), Equals, "v1")
	c.Check(grubEditenvGet(c, "k2"), Equals, "v2")
}
