// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2019-2022 Canonical Ltd
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

package keys_test

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	. "gopkg.in/check.v1"

	"github.com/ddkwork/golibrary/mylog"
	"github.com/snapcore/snapd/secboot/keys"
	"github.com/snapcore/snapd/testutil"
)

func TestSecboot(t *testing.T) { TestingT(t) }

type keysSuite struct {
	dir string
}

var _ = Suite(&keysSuite{})

func (s *keysSuite) SetUpTest(c *C) {
	s.dir = c.MkDir()
}

func (s *keysSuite) TestRecoveryKeySave(c *C) {
	kf := filepath.Join(s.dir, "test-key")
	kfNested := filepath.Join(s.dir, "deeply/nested/test-key")

	rkey := keys.RecoveryKey{0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 255}
	mylog.Check(rkey.Save(kf))

	c.Assert(kf, testutil.FileEquals, rkey[:])

	fileInfo := mylog.Check2(os.Stat(kf))

	c.Assert(fileInfo.Mode(), Equals, os.FileMode(0600))
	mylog.Check(rkey.Save(kfNested))

	c.Assert(kfNested, testutil.FileEquals, rkey[:])
	di := mylog.Check2(os.Stat(filepath.Dir(kfNested)))

	c.Assert(di.Mode().Perm(), Equals, os.FileMode(0755))
}

func (s *keysSuite) TestEncryptionKeySave(c *C) {
	kf := filepath.Join(s.dir, "test-key")
	kfNested := filepath.Join(s.dir, "deeply/nested/test-key")

	ekey := keys.EncryptionKey{0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 255}
	mylog.Check(ekey.Save(kf))

	c.Assert(kf, testutil.FileEquals, []byte(ekey))

	fileInfo := mylog.Check2(os.Stat(kf))

	c.Assert(fileInfo.Mode(), Equals, os.FileMode(0600))
	mylog.Check(ekey.Save(kfNested))

	c.Assert(kfNested, testutil.FileEquals, []byte(ekey))
	di := mylog.Check2(os.Stat(filepath.Dir(kfNested)))

	c.Assert(di.Mode().Perm(), Equals, os.FileMode(0755))
}

func (s *keysSuite) TestNewAuxKeyHappy(c *C) {
	restore := keys.MockRandRead(func(p []byte) (int, error) {
		for i := range p {
			p[i] = byte(i % 10)
		}
		return len(p), nil
	})
	defer restore()

	auxKey := mylog.Check2(keys.NewAuxKey())

	c.Assert(auxKey, HasLen, 32)
	c.Check(auxKey[:], DeepEquals, []byte{
		0x0, 0x1, 0x2, 0x3, 0x4, 0x5, 0x6, 0x7, 0x8, 0x9,
		0x0, 0x1, 0x2, 0x3, 0x4, 0x5, 0x6, 0x7, 0x8, 0x9,
		0x0, 0x1, 0x2, 0x3, 0x4, 0x5, 0x6, 0x7, 0x8, 0x9,
		0x0, 0x1,
	})
}

func (s *keysSuite) TestNewAuxKeySad(c *C) {
	restore := keys.MockRandRead(func(p []byte) (int, error) {
		return 0, fmt.Errorf("fail")
	})
	defer restore()

	_ := mylog.Check2(keys.NewAuxKey())
	c.Check(err, ErrorMatches, "fail")
}
