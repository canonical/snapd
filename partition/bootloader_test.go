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
	"os"
	"testing"

	. "gopkg.in/check.v1"

	"github.com/ubuntu-core/snappy/dirs"
)

// Hook up check.v1 into the "go test" runner
func Test(t *testing.T) { TestingT(t) }

// partition specific testsuite
type PartitionTestSuite struct {
	tempdir string
}

var _ = Suite(&PartitionTestSuite{})

func mockRunCommand(args ...string) (err error) {
	return err
}

func (s *PartitionTestSuite) SetUpTest(c *C) {
	s.tempdir = c.MkDir()

	// global roto
	dirs.SetRootDir(s.tempdir)
	err := os.MkdirAll(bootloaderGrubDir(), 0755)
	c.Assert(err, IsNil)
	err = os.MkdirAll(bootloaderUbootDir(), 0755)
	c.Assert(err, IsNil)
}

func (s *PartitionTestSuite) TearDownTest(c *C) {
	os.RemoveAll(s.tempdir)

	// always restore what we might have mocked away
	runCommand = runCommandImpl
	bootloader = bootloaderImpl
}

type mockBootloader struct {
	BootVars map[string]string
}

func newMockBootloader() *mockBootloader {
	return &mockBootloader{
		BootVars: make(map[string]string),
	}
}

func (b *mockBootloader) Name() bootloaderName {
	return "mocky"
}
func (b *mockBootloader) GetBootVar(name string) (string, error) {
	return b.BootVars[name], nil
}
func (b *mockBootloader) SetBootVar(name, value string) error {
	b.BootVars[name] = value
	return nil
}
func (b *mockBootloader) BootDir() string {
	return ""
}

func (s *PartitionTestSuite) TestMarkBootSuccessfulAllSnap(c *C) {
	runCommand = mockRunCommand

	b := newMockBootloader()
	b.BootVars["snappy_os"] = "os1"
	b.BootVars["snappy_kernel"] = "k1"
	err := markBootSuccessful(b)
	c.Assert(err, IsNil)
	c.Assert(b.BootVars, DeepEquals, map[string]string{
		"snappy_mode":        "regular",
		"snappy_trial_boot":  "0",
		"snappy_kernel":      "k1",
		"snappy_good_kernel": "k1",
		"snappy_os":          "os1",
		"snappy_good_os":     "os1",
	})
}
