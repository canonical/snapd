// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2019-2021 Canonical Ltd
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
	"fmt"
	"os"
	"path/filepath"
	"testing"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/secboot"
	"github.com/snapcore/snapd/testutil"
)

func TestSecboot(t *testing.T) { TestingT(t) }

type encryptSuite struct {
	dir string
}

var _ = Suite(&encryptSuite{})

func (s *encryptSuite) SetUpTest(c *C) {
	s.dir = c.MkDir()
}

func (s *encryptSuite) TestRecoveryKeySave(c *C) {
	kf := filepath.Join(s.dir, "test-key")
	kfNested := filepath.Join(s.dir, "deeply/nested/test-key")

	rkey := secboot.RecoveryKey{0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 255}
	err := rkey.Save(kf)
	c.Assert(err, IsNil)
	c.Assert(kf, testutil.FileEquals, rkey[:])

	fileInfo, err := os.Stat(kf)
	c.Assert(err, IsNil)
	c.Assert(fileInfo.Mode(), Equals, os.FileMode(0600))

	err = rkey.Save(kfNested)
	c.Assert(err, IsNil)
	c.Assert(kfNested, testutil.FileEquals, rkey[:])
	di, err := os.Stat(filepath.Dir(kfNested))
	c.Assert(err, IsNil)
	c.Assert(di.Mode().Perm(), Equals, os.FileMode(0755))
}

func (s *encryptSuite) TestEncryptionKeySave(c *C) {
	kf := filepath.Join(s.dir, "test-key")
	kfNested := filepath.Join(s.dir, "deeply/nested/test-key")

	ekey := secboot.EncryptionKey{0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 255}
	err := ekey.Save(kf)
	c.Assert(err, IsNil)
	c.Assert(kf, testutil.FileEquals, []byte(ekey))

	fileInfo, err := os.Stat(kf)
	c.Assert(err, IsNil)
	c.Assert(fileInfo.Mode(), Equals, os.FileMode(0600))

	err = ekey.Save(kfNested)
	c.Assert(err, IsNil)
	c.Assert(kfNested, testutil.FileEquals, []byte(ekey))
	di, err := os.Stat(filepath.Dir(kfNested))
	c.Assert(err, IsNil)
	c.Assert(di.Mode().Perm(), Equals, os.FileMode(0755))
}

func (s *encryptSuite) TestNewAuxKeyHappy(c *C) {
	restore := secboot.MockRandRead(func(p []byte) (int, error) {
		for i := range p {
			p[i] = byte(i % 10)
		}
		return len(p), nil
	})
	defer restore()

	auxKey, err := secboot.NewAuxKey()
	c.Assert(err, IsNil)
	c.Assert(auxKey, HasLen, 32)
	c.Check(auxKey[:], DeepEquals, []byte{
		0x0, 0x1, 0x2, 0x3, 0x4, 0x5, 0x6, 0x7, 0x8, 0x9,
		0x0, 0x1, 0x2, 0x3, 0x4, 0x5, 0x6, 0x7, 0x8, 0x9,
		0x0, 0x1, 0x2, 0x3, 0x4, 0x5, 0x6, 0x7, 0x8, 0x9,
		0x0, 0x1,
	})
}

func (s *encryptSuite) TestNewAuxKeySad(c *C) {
	restore := secboot.MockRandRead(func(p []byte) (int, error) {
		return 0, fmt.Errorf("fail")
	})
	defer restore()

	_, err := secboot.NewAuxKey()
	c.Check(err, ErrorMatches, "fail")
}
