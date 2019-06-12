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

package bootloader_test

import (
	"fmt"
	"os/exec"
	"path/filepath"

	"github.com/mvo5/goconfigparser"
	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/bootloader"
	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/osutil"
)

type grubTestSuite struct{}

var _ = Suite(&grubTestSuite{})

func (s *grubTestSuite) SetUpTest(c *C) {
	dirs.SetRootDir(c.MkDir())
	bootloader.MockGrubFiles(c)
}

func (s *grubTestSuite) TearDownTest(c *C) {
	dirs.SetRootDir("")
}

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

func (s *grubTestSuite) makeFakeGrubEnv(c *C) {
	grubEditenvSet(c, "k", "v")
}

func (s *grubTestSuite) TestNewGrubNoGrubReturnsNil(c *C) {
	dirs.GlobalRootDir = "/something/not/there"

	g := bootloader.NewGrub()
	c.Assert(g, IsNil)
}

func (s *grubTestSuite) TestNewGrub(c *C) {
	s.makeFakeGrubEnv(c)

	g := bootloader.NewGrub()
	c.Assert(g, NotNil)
	c.Assert(g.Name(), Equals, "grub")
}

func (s *grubTestSuite) TestGetBootloaderWithGrub(c *C) {
	s.makeFakeGrubEnv(c)

	bootloader, err := bootloader.Find()
	c.Assert(err, IsNil)
	c.Assert(bootloader.Name(), Equals, "grub")
}

func (s *grubTestSuite) TestGetBootVer(c *C) {
	s.makeFakeGrubEnv(c)
	grubEditenvSet(c, "snap_mode", "regular")

	g := bootloader.NewGrub()
	v, err := g.GetBootVars("snap_mode")
	c.Assert(err, IsNil)
	c.Check(v, HasLen, 1)
	c.Check(v["snap_mode"], Equals, "regular")
}

func (s *grubTestSuite) TestSetBootVer(c *C) {
	s.makeFakeGrubEnv(c)

	g := bootloader.NewGrub()
	err := g.SetBootVars(map[string]string{
		"k1": "v1",
		"k2": "v2",
	})
	c.Assert(err, IsNil)

	c.Check(grubEditenvGet(c, "k1"), Equals, "v1")
	c.Check(grubEditenvGet(c, "k2"), Equals, "v2")
}
