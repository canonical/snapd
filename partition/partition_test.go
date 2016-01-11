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
	"io/ioutil"
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

	// custom mount target
	mountTarget = c.MkDir()

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
	cacheDir = cacheDirReal
	mountTarget = mountTargetReal
}

func makeHardwareYaml(c *C, hardwareYaml string) (outPath string) {
	tmp, err := ioutil.TempFile(c.MkDir(), "hw-")
	c.Assert(err, IsNil)
	defer tmp.Close()

	if hardwareYaml == "" {
		hardwareYaml = `
kernel: assets/vmlinuz
initrd: assets/initrd.img
dtbs: assets/dtbs
partition-layout: system-AB
bootloader: u-boot
`
	}
	_, err = tmp.Write([]byte(hardwareYaml))
	c.Assert(err, IsNil)

	return tmp.Name()
}

// mock bootloader for the tests
type mockBootloader struct {
	HandleAssetsCalled              bool
	MarkCurrentBootSuccessfulCalled bool
	SyncBootFilesCalled             bool
	BootVars                        map[string]string
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
	bootloader = func(p *Partition) (bootLoader, error) {
		return b, nil
	}

	p := New()
	c.Assert(c, NotNil)

	b.BootVars["snappy_os"] = "os1"
	b.BootVars["snappy_kernel"] = "k1"
	err := p.MarkBootSuccessful()
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
