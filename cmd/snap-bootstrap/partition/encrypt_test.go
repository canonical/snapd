// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2019 Canonical Ltd
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
	"os"
	"path/filepath"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/cmd/snap-bootstrap/partition"
	"github.com/snapcore/snapd/gadget"
	"github.com/snapcore/snapd/testutil"
)

type encryptionSuite struct {
	testutil.BaseTest

	mockCryptsetup *testutil.MockCmd
	tempDir        string
}

var _ = Suite(&encryptionSuite{})

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

func (s *encryptionSuite) SetUpTest(c *C) {
	s.tempDir = c.MkDir()

	restore := partition.MockTempFile(func(dir, prefix string) (*os.File, error) {
		return os.Create(filepath.Join(s.tempDir, "tempfile"))
	})
	s.AddCleanup(restore)
}

func (s *encryptionSuite) TestEncryptHappy(c *C) {
	s.mockCryptsetup = testutil.MockCommand(c, "cryptsetup", "")
	s.AddCleanup(s.mockCryptsetup.Restore)

	dev := partition.NewEncryptedDevice(&mockDeviceStructure, "some-label")
	// XXX: create empty key to prevent blocking on lack of system entropy
	key := partition.EncryptionKey{}
	err := dev.Encrypt(key)
	c.Assert(err, IsNil)
	c.Assert(dev.Node, Equals, "/dev/mapper/some-label")
	calls := s.mockCryptsetup.Calls()

	tempFile := filepath.Join(s.tempDir, "tempfile")
	c.Assert(calls, DeepEquals, [][]string{
		{"cryptsetup", "-q", "luksFormat", "--type", "luks2", "--pbkdf-memory", "100", "--master-key-file", tempFile, "/dev/node1"},
		{"cryptsetup", "open", "--master-key-file", tempFile, "/dev/node1", "some-label"},
	})

	// test temporary file removal
	c.Assert(tempFile, Not(testutil.FilePresent))
}

func (s *encryptionSuite) TestEncryptFormatError(c *C) {
	s.mockCryptsetup = testutil.MockCommand(c, "cryptsetup", `[ "$2" == "luksFormat" ] && exit 127 || exit 0`)
	s.AddCleanup(s.mockCryptsetup.Restore)

	dev := partition.NewEncryptedDevice(&mockDeviceStructure, "some-label")
	key := partition.EncryptionKey{}
	err := dev.Encrypt(key)
	c.Assert(err, ErrorMatches, "cannot format encrypted device:.*")
}

func (s *encryptionSuite) TestEncryptOpenError(c *C) {
	s.mockCryptsetup = testutil.MockCommand(c, "cryptsetup", `[ "$1" == "open" ] && exit 127 || exit 0`)
	s.AddCleanup(s.mockCryptsetup.Restore)

	dev := partition.NewEncryptedDevice(&mockDeviceStructure, "some-label")
	key := partition.EncryptionKey{}
	err := dev.Encrypt(key)
	c.Assert(err, ErrorMatches, "cannot open encrypted device on /dev/node1:.*")
}
