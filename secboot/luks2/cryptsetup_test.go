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

package luks2_test

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/secboot/luks2"
	"github.com/snapcore/snapd/testutil"
)

func Test(t *testing.T) { TestingT(t) }

type luks2Suite struct {
	testutil.BaseTest

	tmpdir         string
	mockCryptsetup *testutil.MockCmd
}

var _ = Suite(&luks2Suite{})

func (s *luks2Suite) SetUpTest(c *C) {
	s.BaseTest.SetUpTest(c)

	dirs.SetRootDir(c.MkDir())
	s.tmpdir = dirs.GlobalRootDir

	s.mockCryptsetup = testutil.MockCommand(c, "cryptsetup", fmt.Sprintf("cat - > %[1]s/stdout 2>%[1]s/stderr", s.tmpdir))
	s.AddCleanup(s.mockCryptsetup.Restore)
}

func (s *luks2Suite) TestKillSlot(c *C) {
	err := luks2.KillSlot("/my/device", 123, []byte("some-key"))
	c.Check(err, IsNil)
	c.Check(s.mockCryptsetup.Calls(), DeepEquals, [][]string{
		{"cryptsetup", "luksKillSlot", "--type", "luks2", "--key-file", "-", "/my/device", "123"},
	})
	c.Check(filepath.Join(s.tmpdir, "stdout"), testutil.FileEquals, "some-key")
	c.Check(filepath.Join(s.tmpdir, "stderr"), testutil.FileEquals, "")
}

func (s *luks2Suite) TestAddKeyHappy(c *C) {
	err := os.MkdirAll(filepath.Join(s.tmpdir, "run"), 0755)
	c.Assert(err, IsNil)

	mockCryptsetup := testutil.MockCommand(c, "cryptsetup", fmt.Sprintf(`
FIFO="$5"
cat "$FIFO" > %[1]s/fifo
cat - > %[1]s/stdout 2>%[1]s/stderr
`, s.tmpdir))
	defer mockCryptsetup.Restore()

	err = luks2.AddKey("/my/device", []byte("old-key"), []byte("new-key"), nil)
	c.Check(err, IsNil)
	c.Check(mockCryptsetup.Calls(), HasLen, 1)
	fifoPath := mockCryptsetup.Calls()[0][5]
	c.Check(mockCryptsetup.Calls(), DeepEquals, [][]string{
		{"cryptsetup", "luksAddKey", "--type", "luks2", "--key-file", fifoPath, "--pbkdf", "argon2i", "/my/device", "-"},
	})
	c.Check(filepath.Join(s.tmpdir, "stdout"), testutil.FileEquals, "new-key")
	c.Check(filepath.Join(s.tmpdir, "stderr"), testutil.FileEquals, "")
	c.Check(filepath.Join(s.tmpdir, "fifo"), testutil.FileEquals, "old-key")
}

func (s *luks2Suite) TestAddKeyBadCryptsetup(c *C) {
	err := os.MkdirAll(filepath.Join(s.tmpdir, "run"), 0755)
	c.Assert(err, IsNil)

	mockCryptsetup := testutil.MockCommand(c, "cryptsetup", "echo some-error; exit  1")
	defer mockCryptsetup.Restore()

	err = luks2.AddKey("/my/device", []byte("old-key"), []byte("new-key"), nil)
	c.Check(err, ErrorMatches, "cryptsetup failed with: some-error")
}

func (s *luks2Suite) TestAddKeyBadWriteExistingKeyToFifo(c *C) {
	err := os.MkdirAll(filepath.Join(s.tmpdir, "run"), 0755)
	c.Assert(err, IsNil)

	mockCryptsetup := testutil.MockCommand(c, "cryptsetup", fmt.Sprintf(`
FIFO="$5"
cat "$FIFO" > %[1]s/fifo
cat - > %[1]s/stdout 2>%[1]s/stderr
`, s.tmpdir))
	defer mockCryptsetup.Restore()

	restore := luks2.MockWriteExistingKeyToFifo(func(string, []byte) error {
		return fmt.Errorf("writeExistingKeyToFifo error")
	})
	defer restore()

	err = luks2.AddKey("/my/device", []byte("old-key"), []byte("new-key"), nil)
	c.Check(err, ErrorMatches, `cryptsetup failed with: .* \(fifo failed with: writeExistingKeyToFifo error\)`)
}
