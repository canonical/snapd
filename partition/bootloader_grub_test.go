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

	"github.com/ubuntu-core/snappy/dirs"

	. "gopkg.in/check.v1"
)

func mockGrubFile(c *C, newPath string, mode os.FileMode) {
	err := ioutil.WriteFile(newPath, []byte(""), mode)
	c.Assert(err, IsNil)
}

func (s *PartitionTestSuite) makeFakeGrubEnv(c *C) {
	// create bootloader
	err := os.MkdirAll(grubDir(), 0755)
	c.Assert(err, IsNil)

	// these files just needs to exist
	mockGrubFile(c, grubConfigFile(), 0644)
	mockGrubFile(c, grubEnvFile(), 0644)

	// do not run commands for real
	runCommand = mockRunCommandWithCapture
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

type singleCommand []string

var allCommands = []singleCommand{}

func mockRunCommandWithCapture(args ...string) (err error) {
	allCommands = append(allCommands, args)
	return nil
}

func mockGrubEditenvList(cmd ...string) (string, error) {
	mockGrubEditenvOutput := fmt.Sprintf("%s=regular", bootloaderBootmodeVar)
	return mockGrubEditenvOutput, nil
}

func (s *PartitionTestSuite) TestGetBootVer(c *C) {
	s.makeFakeGrubEnv(c)
	runCommandWithStdout = mockGrubEditenvList

	g := newGrub()

	v, err := g.GetBootVar(bootloaderBootmodeVar)
	c.Assert(err, IsNil)
	c.Assert(v, Equals, "regular")
}

func (s *PartitionTestSuite) TestGetBootloaderWithGrub(c *C) {
	s.makeFakeGrubEnv(c)

	bootloader, err := bootloader()
	c.Assert(err, IsNil)
	c.Assert(bootloader, FitsTypeOf, &grub{})
}
