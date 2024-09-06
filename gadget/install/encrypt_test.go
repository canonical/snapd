// -*- Mode: Go; indent-tabs-mode: t -*-
//go:build !nosecboot

/*
 * Copyright (C) 2020, 2024 Canonical Ltd
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
	"github.com/snapcore/snapd/gadget/install"
	"github.com/snapcore/snapd/secboot"
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
		defer install.MockCryptsetupOpen(func(key secboot.DiskUnlockKey, node, name string) error {
			if tc.mockedOpenErr != "" {
				return fmt.Errorf(tc.mockedOpenErr)
			}
			return nil
		})()

		defer install.MockCryptsetupClose(func(name string) error {
			return nil
		})()

		calls := 0
		restore := install.MockSecbootFormatEncryptedDevice(func(key []byte, encType secboot.EncryptionType, label, node string) error {
			calls++
			c.Assert(key, DeepEquals, []byte(s.mockedEncryptionKey))
			c.Assert(label, Equals, "some-label-enc")
			c.Assert(node, Equals, "/dev/node1")
			return tc.mockedFormatErr
		})
		defer restore()

		dev, err := install.NewEncryptedDeviceLUKS("/dev/node1", secboot.EncryptionTypeLUKS, secboot.DiskUnlockKey(s.mockedEncryptionKey), "some-label", "some-label")
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
	}
}
