// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2019-2020 Canonical Ltd
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
	"io/ioutil"
	"os"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/secboot"
)

type encryptionKeyTestSuite struct{}

var _ = Suite(&encryptionKeyTestSuite{})

func (s *encryptionKeyTestSuite) TestRecoveryKeySave(c *C) {
	rkey := secboot.RecoveryKey{0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 255}
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
