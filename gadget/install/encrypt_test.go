// -*- Mode: Go; indent-tabs-mode: t -*-
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
	"github.com/snapcore/snapd/secboot"
	"github.com/snapcore/snapd/testutil"
)

type encryptSuite struct {
	testutil.BaseTest

	mockCryptsetup *testutil.MockCmd

	mockedEncryptionKey secboot.EncryptionKey
	mockedRecoveryKey   secboot.RecoveryKey
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

	// create empty key to prevent blocking on lack of system entropy
	s.mockedEncryptionKey = secboot.EncryptionKey{}
	for i := range s.mockedEncryptionKey {
		s.mockedEncryptionKey[i] = byte(i)
	}
	s.mockedRecoveryKey = secboot.RecoveryKey{15, 14, 13, 12, 11, 10, 9, 8, 7, 6, 5, 4, 3, 2, 1, 0}
}

func (s *encryptSuite) TestNewEncryptedDevice(c *C) {
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
		restore := install.MockSecbootFormatEncryptedDevice(func(key secboot.EncryptionKey, label, node string) error {
			calls++
			c.Assert(key, DeepEquals, s.mockedEncryptionKey)
			c.Assert(label, Equals, "some-label-enc")
			c.Assert(node, Equals, "/dev/node1")
			return tc.mockedFormatErr
		})
		defer restore()

		dev, err := install.NewEncryptedDevice(&mockDeviceStructure, s.mockedEncryptionKey, "some-label")
		c.Assert(calls, Equals, 1)
		if tc.expectedErr == "" {
			c.Assert(err, IsNil)
		} else {
			c.Assert(err, ErrorMatches, tc.expectedErr)
			continue
		}
		c.Assert(dev.Node, Equals, "/dev/mapper/some-label")

		err = dev.Close()
		c.Assert(err, IsNil)

		c.Assert(s.mockCryptsetup.Calls(), DeepEquals, [][]string{
			{"cryptsetup", "open", "--key-file", "-", "/dev/node1", "some-label"},
			{"cryptsetup", "close", "some-label"},
		})
	}
}

func (s *encryptSuite) TestAddRecoveryKey(c *C) {
	for _, tc := range []struct {
		mockedAddErr error
		expectedErr  string
	}{
		{mockedAddErr: nil, expectedErr: ""},
		{mockedAddErr: errors.New("add key error"), expectedErr: "add key error"},
	} {
		s.mockCryptsetup = testutil.MockCommand(c, "cryptsetup", "")
		s.AddCleanup(s.mockCryptsetup.Restore)

		restore := install.MockSecbootFormatEncryptedDevice(func(key secboot.EncryptionKey, label, node string) error {
			return nil
		})
		defer restore()

		calls := 0
		restore = install.MockSecbootAddRecoveryKey(func(key secboot.EncryptionKey, rkey secboot.RecoveryKey, node string) error {
			calls++
			c.Assert(key, DeepEquals, s.mockedEncryptionKey)
			c.Assert(rkey, DeepEquals, s.mockedRecoveryKey)
			c.Assert(node, Equals, "/dev/node1")
			return tc.mockedAddErr
		})
		defer restore()

		dev, err := install.NewEncryptedDevice(&mockDeviceStructure, s.mockedEncryptionKey, "some-label")
		c.Assert(err, IsNil)

		err = dev.AddRecoveryKey(s.mockedEncryptionKey, s.mockedRecoveryKey)
		c.Assert(calls, Equals, 1)
		if tc.expectedErr == "" {
			c.Assert(err, IsNil)
		} else {
			c.Assert(err, ErrorMatches, tc.expectedErr)
			continue
		}

		err = dev.Close()
		c.Assert(err, IsNil)

		c.Assert(s.mockCryptsetup.Calls(), DeepEquals, [][]string{
			{"cryptsetup", "open", "--key-file", "-", "/dev/node1", "some-label"},
			{"cryptsetup", "close", "some-label"},
		})
	}
}
