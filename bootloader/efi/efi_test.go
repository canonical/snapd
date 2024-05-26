// -*- Mode: Go; indent-tabs-mode: t -*-

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

package efi_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	. "gopkg.in/check.v1"

	"github.com/ddkwork/golibrary/mylog"
	"github.com/snapcore/snapd/bootloader/bootloadertest"
	"github.com/snapcore/snapd/bootloader/efi"
	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/testutil"
)

type efiVarsSuite struct {
	testutil.BaseTest

	rootdir string
}

var _ = Suite(&efiVarsSuite{})

func TestBoot(t *testing.T) { TestingT(t) }

func (s *efiVarsSuite) SetUpTest(c *C) {
	s.BaseTest.SetUpTest(c)

	s.rootdir = c.MkDir()
	dirs.SetRootDir(s.rootdir)
	s.AddCleanup(func() { dirs.SetRootDir("") })
	mylog.Check(os.MkdirAll(filepath.Join(s.rootdir, "/sys/firmware/efi/efivars"), 0755))


	efivarfsMount := `
38 24 0:32 / /sys/firmware/efi/efivars rw,nosuid,nodev,noexec,relatime shared:13 - efivarfs efivarfs rw
`
	restore := osutil.MockMountInfo(strings.TrimSpace(efivarfsMount))
	s.AddCleanup(restore)
}

func (s *efiVarsSuite) TestNoEFISystem(c *C) {
	// no efivarfs
	osutil.MockMountInfo("")

	_, _ := mylog.Check3(efi.ReadVarBytes("my-cool-efi-var"))
	c.Check(err, Equals, efi.ErrNoEFISystem)

	_, _ = mylog.Check3(efi.ReadVarString("my-cool-efi-var"))
	c.Check(err, Equals, efi.ErrNoEFISystem)
}

func (s *efiVarsSuite) TestSizeError(c *C) {
	// mock the efi var file
	varPath := filepath.Join(s.rootdir, "/sys/firmware/efi/efivars", "my-cool-efi-var")
	mylog.Check(os.WriteFile(varPath, []byte("\x06"), 0644))


	_, _ = mylog.Check3(efi.ReadVarBytes("my-cool-efi-var"))
	c.Check(err, ErrorMatches, `cannot read EFI var "my-cool-efi-var": unexpected size: 1`)
}

func (s *efiVarsSuite) TestReadVarBytes(c *C) {
	// mock the efi var file
	varPath := filepath.Join(s.rootdir, "/sys/firmware/efi/efivars", "my-cool-efi-var")
	mylog.Check(os.WriteFile(varPath, []byte("\x06\x00\x00\x00\x01"), 0644))


	data, attr := mylog.Check3(efi.ReadVarBytes("my-cool-efi-var"))

	c.Check(attr, Equals, efi.VariableBootServiceAccess|efi.VariableRuntimeAccess)
	c.Assert(string(data), Equals, "\x01")
}

func (s *efiVarsSuite) TestReadVarString(c *C) {
	// mock the efi var file
	varPath := filepath.Join(s.rootdir, "/sys/firmware/efi/efivars", "my-cool-efi-var")
	mylog.Check(os.WriteFile(varPath, []byte("\x06\x00\x00\x00A\x009\x00F\x005\x00C\x009\x004\x009\x00-\x00A\x00B\x008\x009\x00-\x005\x00B\x004\x007\x00-\x00A\x007\x00B\x00F\x00-\x005\x006\x00D\x00D\x002\x008\x00F\x009\x006\x00E\x006\x005\x00\x00\x00"), 0644))


	data, attr := mylog.Check3(efi.ReadVarString("my-cool-efi-var"))

	c.Check(attr, Equals, efi.VariableBootServiceAccess|efi.VariableRuntimeAccess)
	c.Assert(data, Equals, "A9F5C949-AB89-5B47-A7BF-56DD28F96E65")
}

func (s *efiVarsSuite) TestEmpty(c *C) {
	// mock the efi var file
	varPath := filepath.Join(s.rootdir, "/sys/firmware/efi/efivars", "my-cool-efi-var")
	mylog.Check(os.WriteFile(varPath, []byte("\x06\x00\x00\x00"), 0644))


	b, _ := mylog.Check3(efi.ReadVarBytes("my-cool-efi-var"))

	c.Check(b, HasLen, 0)

	v, _ := mylog.Check3(efi.ReadVarString("my-cool-efi-var"))

	c.Check(v, HasLen, 0)
}

func (s *efiVarsSuite) TestMockVars(c *C) {
	restore := efi.MockVars(map[string][]byte{
		"a": []byte("\x01"),
		"b": []byte("\x02"),
	}, map[string]efi.VariableAttr{
		"b": efi.VariableNonVolatile | efi.VariableRuntimeAccess | efi.VariableBootServiceAccess,
	})
	defer restore()

	b, attr := mylog.Check3(efi.ReadVarBytes("a"))

	c.Check(attr, Equals, efi.VariableBootServiceAccess|efi.VariableRuntimeAccess)
	c.Assert(string(b), Equals, "\x01")

	b, attr = mylog.Check3(efi.ReadVarBytes("b"))

	c.Check(attr, Equals, efi.VariableBootServiceAccess|efi.VariableRuntimeAccess|efi.VariableNonVolatile)
	c.Assert(string(b), Equals, "\x02")
}

func (s *efiVarsSuite) TestMockStringVars(c *C) {
	restore := efi.MockVars(map[string][]byte{
		"a": bootloadertest.UTF16Bytes("foo-bar-baz"),
	}, nil)
	defer restore()

	v, attr := mylog.Check3(efi.ReadVarString("a"))

	c.Check(attr, Equals, efi.VariableBootServiceAccess|efi.VariableRuntimeAccess)
	c.Assert(v, Equals, "foo-bar-baz")
}

func (s *efiVarsSuite) TestMockVarsNoEFISystem(c *C) {
	restore := efi.MockVars(nil, nil)
	defer restore()

	_, _ := mylog.Check3(efi.ReadVarBytes("a"))
	c.Check(err, Equals, efi.ErrNoEFISystem)
}

func (s *efiVarsSuite) TestStringOddSize(c *C) {
	restore := efi.MockVars(map[string][]byte{
		"a": []byte("\x0a"),
	}, nil)
	defer restore()

	_, _ := mylog.Check3(efi.ReadVarString("a"))
	c.Check(err, ErrorMatches, `EFI var "a" is not a valid UTF16 string, it has an extra byte`)
}
