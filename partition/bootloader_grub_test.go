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
	"path/filepath"

	"github.com/ubuntu-core/snappy/helpers"

	. "gopkg.in/check.v1"
)

func mockGrubFile(c *C, newPath string, mode os.FileMode) {
	err := ioutil.WriteFile(newPath, []byte(""), mode)
	c.Assert(err, IsNil)
}

func (s *PartitionTestSuite) makeFakeGrubEnv(c *C) {
	// create bootloader
	err := os.MkdirAll(bootloaderGrubDir, 0755)
	c.Assert(err, IsNil)

	// these files just needs to exist
	mockGrubFile(c, bootloaderGrubConfigFile, 0644)
	mockGrubFile(c, bootloaderGrubEnvFile, 0644)

	// do not run commands for real
	runCommand = mockRunCommandWithCapture
}

func (s *PartitionTestSuite) TestNewGrubNoGrubReturnsNil(c *C) {
	bootloaderGrubConfigFile = "no-such-dir"

	partition := New()
	g := newGrub(partition)
	c.Assert(g, IsNil)
}

func (s *PartitionTestSuite) TestNewGrub(c *C) {
	s.makeFakeGrubEnv(c)

	partition := New()
	g := newGrub(partition)
	c.Assert(g, NotNil)
	c.Assert(g.Name(), Equals, bootloaderNameGrub)
}

type singleCommand []string

var allCommands = []singleCommand{}

func mockRunCommandWithCapture(args ...string) (err error) {
	allCommands = append(allCommands, args)
	return nil
}

func (s *PartitionTestSuite) TestToggleRootFS(c *C) {
	s.makeFakeGrubEnv(c)
	allCommands = []singleCommand{}

	partition := New()
	g := newGrub(partition)
	c.Assert(g, NotNil)
	err := g.ToggleRootFS("b")
	c.Assert(err, IsNil)

	// this is always called
	mp := singleCommand{"/bin/mountpoint", mountTarget}
	c.Assert(allCommands[0], DeepEquals, mp)

	expectedGrubSet := singleCommand{bootloaderGrubEnvCmd, bootloaderGrubEnvFile, "set", "snappy_mode=try"}
	c.Assert(allCommands[1], DeepEquals, expectedGrubSet)

	// the https://developer.ubuntu.com/en/snappy/porting guide says
	// we always use the short names
	expectedGrubSet = singleCommand{bootloaderGrubEnvCmd, bootloaderGrubEnvFile, "set", "snappy_ab=b"}
	c.Assert(allCommands[2], DeepEquals, expectedGrubSet)

	c.Assert(len(allCommands), Equals, 3)
}

func mockGrubEditenvList(cmd ...string) (string, error) {
	mockGrubEditenvOutput := fmt.Sprintf("%s=regular", bootloaderBootmodeVar)
	return mockGrubEditenvOutput, nil
}

func (s *PartitionTestSuite) TestGetBootVer(c *C) {
	s.makeFakeGrubEnv(c)
	runCommandWithStdout = mockGrubEditenvList

	partition := New()
	g := newGrub(partition)

	v, err := g.GetBootVar(bootloaderBootmodeVar)
	c.Assert(err, IsNil)
	c.Assert(v, Equals, "regular")
}

func (s *PartitionTestSuite) TestGetBootloaderWithGrub(c *C) {
	s.makeFakeGrubEnv(c)
	p := New()
	bootloader, err := bootloader(p)
	c.Assert(err, IsNil)
	c.Assert(bootloader.Name(), Equals, bootloaderNameGrub)
}

func (s *PartitionTestSuite) TestGrubMarkCurrentBootSuccessful(c *C) {
	s.makeFakeGrubEnv(c)
	allCommands = []singleCommand{}

	partition := New()
	g := newGrub(partition)
	c.Assert(g, NotNil)
	err := g.MarkCurrentBootSuccessful("a")
	c.Assert(err, IsNil)

	// this is always called
	mp := singleCommand{"/bin/mountpoint", mountTarget}
	c.Assert(allCommands[0], DeepEquals, mp)

	expectedGrubSet := singleCommand{bootloaderGrubEnvCmd, bootloaderGrubEnvFile, "set", "snappy_trial_boot=0"}

	c.Assert(allCommands[1], DeepEquals, expectedGrubSet)

	expectedGrubSet2 := singleCommand{bootloaderGrubEnvCmd, bootloaderGrubEnvFile, "set", "snappy_ab=a"}

	c.Assert(allCommands[2], DeepEquals, expectedGrubSet2)

	expectedGrubSet3 := singleCommand{bootloaderGrubEnvCmd, bootloaderGrubEnvFile, "set", "snappy_mode=regular"}

	c.Assert(allCommands[3], DeepEquals, expectedGrubSet3)

}

func (s *PartitionTestSuite) TestSyncBootFilesWithAssets(c *C) {
	err := os.MkdirAll(bootloaderGrubDir, 0755)
	c.Assert(err, IsNil)

	runCommand = mockRunCommand
	b := grub{
		bootloaderType{
			currentBootPath: c.MkDir(),
			otherBootPath:   c.MkDir(),
			bootloaderDir:   c.MkDir(),
		},
	}

	bootfile := filepath.Join(c.MkDir(), "bootfile")
	err = ioutil.WriteFile(bootfile, []byte(bootfile), 0644)
	c.Assert(err, IsNil)

	bootassets := map[string]string{
		bootfile: filepath.Base(bootfile),
	}

	err = b.SyncBootFiles(bootassets)
	c.Assert(err, IsNil)

	dst := filepath.Join(b.bootloaderDir, bootassets[bootfile])
	c.Check(helpers.FileExists(dst), Equals, true)
	c.Check(helpers.FilesAreEqual(bootfile, dst), Equals, true)
}
