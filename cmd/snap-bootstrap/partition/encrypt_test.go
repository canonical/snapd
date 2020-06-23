// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2020 Canonical Ltd
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

package partition_test

import (
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"testing"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/cmd/snap-bootstrap/partition"
	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/gadget"
	"github.com/snapcore/snapd/testutil"
)

func TestPartition(t *testing.T) { TestingT(t) }

type encryptSuite struct {
	testutil.BaseTest

	mockCryptsetup *testutil.MockCmd
}

var _ = Suite(&encryptSuite{})

var mockDeviceStructure = gadget.OnDiskStructure{
	LaidOutStructure: gadget.LaidOutStructure{
		VolumeStructure: &gadget.VolumeStructure{
			Name: "Test structure",
			Size: 0x100000,
		},
		StartOffset: 0,
		Index:       1,
	},
	Node: "/dev/node1",
}

func (s *encryptSuite) SetUpTest(c *C) {
	dirs.SetRootDir(c.MkDir())
	c.Assert(os.MkdirAll(dirs.SnapRunDir, 0755), IsNil)
}

func (s *encryptSuite) TestEncryptHappy(c *C) {
	s.mockCryptsetup = testutil.MockCommand(c, "cryptsetup", "")
	s.AddCleanup(s.mockCryptsetup.Restore)

	// create empty key to prevent blocking on lack of system entropy
	key := partition.EncryptionKey{}
	dev, err := partition.NewEncryptedDevice(&mockDeviceStructure, key, "some-label")
	c.Assert(err, IsNil)
	c.Assert(dev.Node, Equals, "/dev/mapper/some-label")

	c.Assert(s.mockCryptsetup.Calls(), DeepEquals, [][]string{
		{
			"cryptsetup", "-q", "luksFormat", "--type", "luks2", "--key-file", "-",
			"--cipher", "aes-xts-plain64", "--key-size", "512", "--pbkdf", "argon2i",
			"--iter-time", "1", "--label", "some-label-enc", "/dev/node1",
		},
		{
			"cryptsetup", "open", "--key-file", "-", "/dev/node1", "some-label",
		},
	})

	err = dev.Close()
	c.Assert(err, IsNil)
}

func (s *encryptSuite) TestEncryptFormatError(c *C) {
	s.mockCryptsetup = testutil.MockCommand(c, "cryptsetup", `[ "$2" == "luksFormat" ] && exit 127 || exit 0`)
	s.AddCleanup(s.mockCryptsetup.Restore)

	key := partition.EncryptionKey{}
	_, err := partition.NewEncryptedDevice(&mockDeviceStructure, key, "some-label")
	c.Assert(err, ErrorMatches, "cannot format encrypted device:.*")
}

func (s *encryptSuite) TestEncryptOpenError(c *C) {
	s.mockCryptsetup = testutil.MockCommand(c, "cryptsetup", `[ "$1" == "open" ] && exit 127 || exit 0`)
	s.AddCleanup(s.mockCryptsetup.Restore)

	key := partition.EncryptionKey{}
	_, err := partition.NewEncryptedDevice(&mockDeviceStructure, key, "some-label")
	c.Assert(err, ErrorMatches, "cannot open encrypted device on /dev/node1:.*")
}

func (s *encryptSuite) TestEncryptAddKey(c *C) {
	capturedFifo := filepath.Join(c.MkDir(), "captured-stdin")
	s.mockCryptsetup = testutil.MockCommand(c, "cryptsetup", fmt.Sprintf(`[ "$1" == "luksAddKey" ] && cat %s/tmp-rkey > %s || exit 0`, dirs.SnapRunDir, capturedFifo))
	s.AddCleanup(s.mockCryptsetup.Restore)

	key := partition.EncryptionKey{}
	dev, err := partition.NewEncryptedDevice(&mockDeviceStructure, key, "some-label")
	c.Assert(err, IsNil)

	rkey := partition.RecoveryKey{0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15}
	err = dev.AddRecoveryKey(key, rkey)
	c.Assert(err, IsNil)

	c.Assert(s.mockCryptsetup.Calls(), DeepEquals, [][]string{
		{
			"cryptsetup", "-q", "luksFormat", "--type", "luks2", "--key-file", "-",
			"--cipher", "aes-xts-plain64", "--key-size", "512", "--pbkdf", "argon2i",
			"--iter-time", "1", "--label", "some-label-enc", "/dev/node1",
		},
		{
			"cryptsetup", "open", "--key-file", "-", "/dev/node1", "some-label",
		},
		{
			"cryptsetup", "luksAddKey", "/dev/node1", "-q", "--key-file", "-",
			"--key-slot", "1", filepath.Join(dirs.SnapRunDir, "tmp-rkey"),
		},
	})
	c.Assert(capturedFifo, testutil.FileEquals, rkey[:])

	err = dev.Close()
	c.Assert(err, IsNil)
}

func (s *encryptSuite) TestRecoveryKeySave(c *C) {
	rkey := partition.RecoveryKey{0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 255}
	err := rkey.Save("test-key")
	c.Assert(err, IsNil)
	fileInfo, err := os.Stat("test-key")
	c.Assert(err, IsNil)
	c.Assert(fileInfo.Mode(), Equals, os.FileMode(0600))
	data, err := ioutil.ReadFile("test-key")
	c.Assert(err, IsNil)
	c.Assert(data, DeepEquals, rkey[:])
	os.Remove("test-key")
}
