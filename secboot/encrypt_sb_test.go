// -*- Mode: Go; indent-tabs-mode: t -*-
//go:build !nosecboot
// +build !nosecboot

/*
 * Copyright (C) 2021 Canonical Ltd
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

package secboot_test

import (
	"errors"
	"io/ioutil"
	"path/filepath"

	sb "github.com/snapcore/secboot"
	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/secboot"
)

func (s *encryptSuite) TestFormatEncryptedDevice(c *C) {
	for _, tc := range []struct {
		initErr error
		err     string
	}{
		{initErr: nil, err: ""},
		{initErr: errors.New("some error"), err: "some error"},
	} {
		// create empty key to prevent blocking on lack of system entropy
		myKey := secboot.EncryptionKey{}
		for i := range myKey {
			myKey[i] = byte(i)
		}

		calls := 0
		restore := secboot.MockSbInitializeLUKS2Container(func(devicePath, label string, key []byte,
			opts *sb.InitializeLUKS2ContainerOptions) error {
			calls++
			c.Assert(devicePath, Equals, "/dev/node")
			c.Assert(label, Equals, "my label")
			c.Assert(key, DeepEquals, []byte(myKey))
			c.Assert(opts, DeepEquals, &sb.InitializeLUKS2ContainerOptions{
				MetadataKiBSize:     2048,
				KeyslotsAreaKiBSize: 2560,
				KDFOptions: &sb.KDFOptions{
					MemoryKiB:       32,
					ForceIterations: 4,
				},
			})
			return tc.initErr
		})
		defer restore()

		err := secboot.FormatEncryptedDevice(myKey, "my label", "/dev/node")
		c.Assert(calls, Equals, 1)
		if tc.err == "" {
			c.Assert(err, IsNil)
		} else {
			c.Assert(err, ErrorMatches, tc.err)
		}
	}
}

const mockedMeminfo = `MemTotal:         929956 kB
CmaTotal:         131072 kB
`

func (s *encryptSuite) TestAddRecoveryKey(c *C) {
	mockedMeminfoFile := filepath.Join(c.MkDir(), "meminfo")
	err := ioutil.WriteFile(mockedMeminfoFile, []byte(mockedMeminfo), 0644)
	c.Assert(err, IsNil)
	restore := osutil.MockProcMeminfo(mockedMeminfoFile)
	defer restore()

	for _, tc := range []struct {
		addErr error
		err    string
	}{
		{addErr: nil, err: ""},
		{addErr: errors.New("some error"), err: "some error"},
	} {
		// create empty key to prevent blocking on lack of system entropy
		myKey := secboot.EncryptionKey{}
		for i := range myKey {
			myKey[i] = byte(i)
		}

		myRecoveryKey := secboot.RecoveryKey{15, 14, 13, 12, 11, 10, 9, 8, 7, 6, 5, 4, 3, 2, 1, 0}

		calls := 0
		restore := secboot.MockSbAddRecoveryKeyToLUKS2Container(func(devicePath string, key []byte, recoveryKey sb.RecoveryKey, opts *sb.KDFOptions) error {
			calls++
			c.Assert(devicePath, Equals, "/dev/node")
			c.Assert(recoveryKey[:], DeepEquals, myRecoveryKey[:])
			c.Assert(key, DeepEquals, []byte(myKey))
			c.Assert(opts, DeepEquals, &sb.KDFOptions{
				// (TotalMem - CmaMem - 384MB hardcoded) / 2
				MemoryKiB:       int((929956 - 131072 - 384*1024) / 2),
				ForceIterations: 4,
			})
			return tc.addErr
		})
		defer restore()

		err := secboot.AddRecoveryKey(myKey, myRecoveryKey, "/dev/node")
		c.Assert(calls, Equals, 1)
		if tc.err == "" {
			c.Assert(err, IsNil)
		} else {
			c.Assert(err, ErrorMatches, tc.err)
		}
	}
}
