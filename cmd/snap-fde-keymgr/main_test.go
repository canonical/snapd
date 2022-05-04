// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2022 Canonical Ltd
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
package main_test

import (
	"bytes"
	"path/filepath"
	"testing"

	. "gopkg.in/check.v1"

	main "github.com/snapcore/snapd/cmd/snap-fde-keymgr"
	"github.com/snapcore/snapd/secboot/keys"
	"github.com/snapcore/snapd/testutil"
)

type mainSuite struct{}

var _ = Suite(&mainSuite{})

func TestT(t *testing.T) {
	TestingT(t)
}

func (s *mainSuite) TestAddKey(c *C) {
	dev := ""
	rkey := keys.RecoveryKey{}
	addCalls := 0
	restore := main.MockAddRecoveryKeyToLUKS(func(recoveryKey keys.RecoveryKey, luksDev string) error {
		addCalls++
		dev = luksDev
		rkey = recoveryKey
		return nil
	})
	defer restore()
	d := c.MkDir()
	err := main.Run([]string{
		"add-recovery-key",
		"--device", "/dev/vda4",
		"--key-file", filepath.Join(d, "recovery.key"),
	})
	c.Assert(err, IsNil)
	c.Check(addCalls, Equals, 1)
	c.Check(dev, Equals, "/dev/vda4")
	c.Check(rkey, Not(DeepEquals), keys.RecoveryKey{})
	c.Assert(filepath.Join(d, "recovery.key"), testutil.FileEquals, rkey[:])

	oldKey := rkey
	// add again, in which case a new key is generated
	err = main.Run([]string{
		"add-recovery-key",
		"--device", "/dev/vda4",
		"--key-file", filepath.Join(d, "recovery.key"),
	})
	c.Assert(err, IsNil)
	c.Check(addCalls, Equals, 2)
	c.Check(dev, Equals, "/dev/vda4")
	c.Check(rkey, Not(DeepEquals), oldKey)
	// file was overwritten
	c.Assert(filepath.Join(d, "recovery.key"), testutil.FileEquals, rkey[:])
}

func (s *mainSuite) TestRemoveKey(c *C) {
	dev := ""
	removeCalls := 0
	restore := main.MockRemoveRecoveryKeyFromLUKS(func(luksDev string) error {
		removeCalls++
		dev = luksDev
		return nil
	})
	defer restore()
	d := c.MkDir()
	err := main.Run([]string{
		"remove-recovery-key",
		"--device", "/dev/vda4",
		"--key-file", filepath.Join(d, "recovery.key"),
	})
	c.Assert(err, IsNil)
	c.Check(removeCalls, Equals, 1)
	c.Check(dev, Equals, "/dev/vda4")
	c.Assert(filepath.Join(d, "recovery.key"), testutil.FileAbsent)
	// again when the recover key file is gone already
	err = main.Run([]string{
		"remove-recovery-key",
		"--device", "/dev/vda4",
		"--key-file", filepath.Join(d, "recovery.key"),
	})
	c.Check(removeCalls, Equals, 2)
	c.Assert(err, IsNil)
}

func (s *mainSuite) TestChangeEncryptionKey(c *C) {
	const all1sKey = `{"key":"MTExMTExMTExMTExMTExMTExMTExMTExMTExMTExMTE="}`
	b := bytes.NewBufferString(all1sKey)
	restore := main.MockOsStdin(b)
	defer restore()
	dev := ""
	changeCalls := 0
	var key []byte
	restore = main.MockChangeLUKSEncryptionKey(func(newKey keys.EncryptionKey, luksDev string) error {
		changeCalls++
		dev = luksDev
		key = newKey
		return nil
	})
	defer restore()
	err := main.Run([]string{
		"change-encryption-key",
		"--device", "/dev/vda4",
	})
	c.Assert(err, IsNil)
	c.Check(changeCalls, Equals, 1)
	c.Check(dev, Equals, "/dev/vda4")
	// secboot encryption key size
	c.Check(key, DeepEquals, bytes.Repeat([]byte("1"), 32))
}
