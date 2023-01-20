// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2023 Canonical Ltd
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
	"testing"

	. "gopkg.in/check.v1"

	main "github.com/snapcore/snapd/cmd/snap-fde-keymgr"
	"github.com/snapcore/snapd/secboot/keys"
)

type mainSuite struct{}

var _ = Suite(&mainSuite{})

func TestT(t *testing.T) {
	TestingT(t)
}

// 1 in ASCII repeated 32 times
const all1sKey = `{"key":"MTExMTExMTExMTExMTExMTExMTExMTExMTExMTExMTE="}`

func (s *mainSuite) TestChangeEncryptionKeyMissingKey(c *C) {
	restore := main.MockChangeEncryptionKey(func(device string, stage, transition bool, key keys.EncryptionKey) error {
		panic("trying to change key without having any key")
	})
	defer restore()

	err := main.Run([]string{
		"change-encryption-key",
		"--device", "/dev/vda4",
		"--stage",
	})

	c.Assert(err, ErrorMatches, "cannot obtain new encryption key:.*")
}

func (s *mainSuite) TestChangeEncryptionKey(c *C) {
	b := bytes.NewBufferString(all1sKey)
	restore := main.MockOsStdin(b)
	defer restore()

	restore = main.MockChangeEncryptionKey(func(device string, stage, transition bool, key keys.EncryptionKey) error {
		c.Check([]uint8(key), DeepEquals, bytes.Repeat([]byte("1"), 32))
		return nil
	})
	defer restore()

	err := main.Run([]string{
		"change-encryption-key",
		"--device", "/dev/vda4",
		"--transition",
	})
	c.Assert(err, IsNil)
}
