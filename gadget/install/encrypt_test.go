// -*- Mode: Go; indent-tabs-mode: t -*-
//go:build !nosecboot
// +build !nosecboot

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

package install_test

import (
	"errors"
	"fmt"
	"os"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/gadget"
	"github.com/snapcore/snapd/gadget/install"
	"github.com/snapcore/snapd/gadget/quantity"
	"github.com/snapcore/snapd/kernel/fde"
	"github.com/snapcore/snapd/secboot/keys"
	"github.com/snapcore/snapd/testutil"
)

type encryptSuite struct {
	testutil.BaseTest

	mockCryptsetup *testutil.MockCmd

	mockedEncryptionKey keys.EncryptionKey
	mockedRecoveryKey   keys.RecoveryKey
}

var _ = Suite(&encryptSuite{})

var mockDeviceStructure = gadget.OnDiskStructure{
	Name:             "Test structure",
	PartitionFSLabel: "some-label",
	StartOffset:      0,
	Size:             3 * quantity.SizeMiB,
	Node:             "/dev/node1",
}

func (s *encryptSuite) SetUpTest(c *C) {
	dirs.SetRootDir(c.MkDir())
	c.Assert(os.MkdirAll(dirs.SnapRunDir, 0755), IsNil)

	// create empty key to prevent blocking on lack of system entropy
	s.mockedEncryptionKey = keys.EncryptionKey{}
	for i := range s.mockedEncryptionKey {
		s.mockedEncryptionKey[i] = byte(i)
	}
	s.mockedRecoveryKey = keys.RecoveryKey{15, 14, 13, 12, 11, 10, 9, 8, 7, 6, 5, 4, 3, 2, 1, 0}
}

func (s *encryptSuite) TestNewEncryptedDeviceLUKS(c *C) {
	for _, tc := range []struct {
		mockedFormatErr error
		mockedOpenErr   string
		expectedErr     string
	}{
		{
			mockedFormatErr: nil,
			mockedOpenErr:   "",
			expectedErr:     "",
		},
		{
			mockedFormatErr: errors.New("format error"),
			mockedOpenErr:   "",
			expectedErr:     "cannot format encrypted device: format error",
		},
		{
			mockedFormatErr: nil,
			mockedOpenErr:   "open error",
			expectedErr:     "cannot open encrypted device on /dev/node1: open error",
		},
	} {
		script := ""
		if tc.mockedOpenErr != "" {
			script = fmt.Sprintf("echo '%s'>&2; exit 1", tc.mockedOpenErr)

		}
		s.mockCryptsetup = testutil.MockCommand(c, "cryptsetup", script)
		s.AddCleanup(s.mockCryptsetup.Restore)

		calls := 0
		restore := install.MockSecbootFormatEncryptedDevice(func(key keys.EncryptionKey, label, node string) error {
			calls++
			c.Assert(key, DeepEquals, s.mockedEncryptionKey)
			c.Assert(label, Equals, "some-label-enc")
			c.Assert(node, Equals, "/dev/node1")
			return tc.mockedFormatErr
		})
		defer restore()

		dev, err := install.NewEncryptedDeviceLUKS(&mockDeviceStructure, s.mockedEncryptionKey, "some-label-enc", "some-label")
		c.Assert(calls, Equals, 1)
		if tc.expectedErr == "" {
			c.Assert(err, IsNil)
		} else {
			c.Assert(err, ErrorMatches, tc.expectedErr)
			continue
		}
		c.Assert(dev.Node(), Equals, "/dev/mapper/some-label")

		err = dev.Close()
		c.Assert(err, IsNil)

		c.Assert(s.mockCryptsetup.Calls(), DeepEquals, [][]string{
			{"cryptsetup", "open", "--key-file", "-", "/dev/node1", "some-label"},
			{"cryptsetup", "close", "some-label"},
		})
	}
}

var mockDeviceStructureForDeviceSetupHook = gadget.OnDiskStructure{
	Name:             "ubuntu-data",
	PartitionFSLabel: "ubuntu-data",
	StartOffset:      0,
	Size:             3 * quantity.SizeMiB,
	Node:             "/dev/node1",
}

func (s *encryptSuite) TestCreateEncryptedDeviceWithSetupHook(c *C) {

	for _, tc := range []struct {
		mockedOpenErr            string
		mockedRunFDESetupHookErr error
		expectedErr              string
	}{
		{
			mockedOpenErr:            "",
			mockedRunFDESetupHookErr: nil,
			expectedErr:              "",
		},
		{
			mockedRunFDESetupHookErr: errors.New("fde-setup hook error"),
			mockedOpenErr:            "",
			expectedErr:              "device setup failed with: fde-setup hook error",
		},

		{
			mockedOpenErr:            "open error",
			mockedRunFDESetupHookErr: nil,
			expectedErr:              `cannot create mapper "ubuntu-data" on /dev/node1: open error`,
		},
	} {
		script := ""
		if tc.mockedOpenErr != "" {
			script = fmt.Sprintf("echo '%s'>&2; exit 1", tc.mockedOpenErr)

		}

		restore := install.MockBootRunFDESetupHook(func(req *fde.SetupRequest) ([]byte, error) {
			return nil, tc.mockedRunFDESetupHookErr
		})
		defer restore()

		mockDmsetup := testutil.MockCommand(c, "dmsetup", script)
		s.AddCleanup(mockDmsetup.Restore)

		dev, err := install.CreateEncryptedDeviceWithSetupHook(&mockDeviceStructureForDeviceSetupHook,
			s.mockedEncryptionKey, "ubuntu-data")
		if tc.expectedErr == "" {
			c.Assert(err, IsNil)
		} else {
			c.Assert(err, ErrorMatches, tc.expectedErr)
			continue
		}
		c.Check(dev.Node(), Equals, "/dev/mapper/ubuntu-data")

		err = dev.Close()
		c.Assert(err, IsNil)

		c.Check(mockDmsetup.Calls(), DeepEquals, [][]string{
			// Caculation is in 512 byte blocks. The total
			// size of the mock device is 3Mb: 2Mb
			// (4096*512) length if left and the offset is
			// 1Mb (2048*512) at the start
			{"dmsetup", "create", "ubuntu-data", "--table", "0 4096 linear /dev/node1 2048"},
			{"dmsetup", "remove", "ubuntu-data"},
		})
	}
}

func (s *encryptSuite) TestCreateEncryptedDeviceWithSetupHookPartitionNameCheck(c *C) {
	mockDeviceStructureBadName := gadget.OnDiskStructure{
		Name:             "ubuntu-data",
		PartitionFSLabel: "ubuntu-data",
		StartOffset:      0,
		Size:             3 * quantity.SizeMiB,
		Node:             "/dev/node1",
	}
	restore := install.MockBootRunFDESetupHook(func(req *fde.SetupRequest) ([]byte, error) {
		c.Error("unexpected call")
		return nil, fmt.Errorf("unexpected call")
	})
	defer restore()

	mockDmsetup := testutil.MockCommand(c, "dmsetup", `echo "unexpected call" >&2; exit 1`)
	s.AddCleanup(mockDmsetup.Restore)

	// pass a name that does not match partition name
	dev, err := install.CreateEncryptedDeviceWithSetupHook(&mockDeviceStructureBadName,
		s.mockedEncryptionKey, "some-name")
	c.Assert(err, ErrorMatches, `cannot use partition name "some-name" for an encrypted structure with partition label "ubuntu-data" or filesystem label "ubuntu-data"`)
	c.Check(dev, IsNil)
	c.Check(mockDmsetup.Calls(), HasLen, 0)
	// make structure name different than the label, which is set to either
	// the implicit value or has already been validated and matches what is
	// expected for the particular role
	mockDeviceStructureBadName.Name = "bad-name"
	dev, err = install.CreateEncryptedDeviceWithSetupHook(&mockDeviceStructureBadName,
		s.mockedEncryptionKey, "bad-name")
	c.Assert(err, ErrorMatches, `cannot use partition name "bad-name" for an encrypted structure with partition label "bad-name" or filesystem label "ubuntu-data"`)
	c.Check(dev, IsNil)
	c.Check(mockDmsetup.Calls(), HasLen, 0)
}
