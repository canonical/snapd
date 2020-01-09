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
	"path/filepath"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/cmd/snap-bootstrap/partition"
	"github.com/snapcore/snapd/gadget"
	"github.com/snapcore/snapd/testutil"
)

type encryptSuite struct {
	testutil.BaseTest

	mockCryptsetup *testutil.MockCmd
	tempDir        string
}

var _ = Suite(&encryptSuite{})

var mockDeviceStructure = partition.DeviceStructure{
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
	s.tempDir = c.MkDir()
}

func (s *encryptSuite) TestEncryptHappy(c *C) {
	s.mockCryptsetup = testutil.MockCommand(c, "cryptsetup", "")
	s.AddCleanup(s.mockCryptsetup.Restore)

	// XXX: create empty key to prevent blocking on lack of system entropy
	key := partition.EncryptionKey{}
	dev, err := partition.NewEncryptedDevice(&mockDeviceStructure, key, "some-label")
	c.Assert(err, IsNil)
	c.Assert(dev.Node, Equals, "/dev/mapper/some-label")

	tempFile := filepath.Join(s.tempDir, "tempfile")
	c.Assert(s.mockCryptsetup.Calls(), DeepEquals, [][]string{
		{"cryptsetup", "-q", "luksFormat", "--type", "luks2", "--key-file", "-", "--pbkdf", "argon2i", "--iter-time", "1", "/dev/node1"},
		{"cryptsetup", "open", "--key-file", "-", "/dev/node1", "some-label"},
	})

	// test temporary file removal
	c.Assert(tempFile, Not(testutil.FilePresent))

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
