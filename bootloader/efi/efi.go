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

// Package efi supports reading EFI variables.
package efi

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"unicode/utf16"

	"github.com/ddkwork/golibrary/mylog"
	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/osutil"
)

var ErrNoEFISystem = errors.New("not a supported EFI system")

type VariableAttr uint32

// see https://git.kernel.org/pub/scm/linux/kernel/git/stable/linux.git/tree/include/linux/efi.h?h=v5.4.32
const (
	VariableNonVolatile       VariableAttr = 0x00000001
	VariableBootServiceAccess VariableAttr = 0x00000002
	VariableRuntimeAccess     VariableAttr = 0x00000004
)

var openEFIVar = openEFIVarImpl

const expectedEFIvarfsDir = "/sys/firmware/efi/efivars"

func openEFIVarImpl(name string) (r io.ReadCloser, attr VariableAttr, size int64, err error) {
	mounts := mylog.Check2(osutil.LoadMountInfo())

	found := false
	for _, mnt := range mounts {
		if mnt.MountDir == expectedEFIvarfsDir {
			if mnt.FsType == "efivarfs" {
				found = true
				break
			}
		}
	}
	if !found {
		return nil, 0, 0, ErrNoEFISystem
	}
	varf := mylog.Check2(os.Open(filepath.Join(dirs.GlobalRootDir, expectedEFIvarfsDir, name)))

	defer func() {
	}()
	fi := mylog.Check2(varf.Stat())

	sz := fi.Size()
	if sz < 4 {
		return nil, 0, 0, fmt.Errorf("unexpected size: %d", sz)
	}
	mylog.Check(binary.Read(varf, binary.LittleEndian, &attr))

	return varf, attr, sz - 4, nil
}

func cannotReadError(name string, err error) error {
	return fmt.Errorf("cannot read EFI var %q: %v", name, err)
}

// ReadVarBytes will attempt to read the bytes of the value of the
// specified EFI variable, specified by its full name composed of the
// variable name and vendor ID. It also returns the attribute value
// attached to it. It expects to use the efivars filesystem at
// /sys/firmware/efi/efivars.
// https://www.kernel.org/doc/Documentation/filesystems/efivarfs.txt
// for more details.
func ReadVarBytes(name string) ([]byte, VariableAttr, error) {
	varf, attr, _ := mylog.Check4(openEFIVar(name))

	defer varf.Close()
	b := mylog.Check2(io.ReadAll(varf))

	return b, attr, nil
}

// ReadVarString will attempt to read the string value of the
// specified EFI variable, specified by its full name composed of the
// variable name and vendor ID. The string value is expected to be
// encoded as UTF16. It also returns the attribute value attached to
// it. It expects to use the efivars filesystem at
// /sys/firmware/efi/efivars.
// https://www.kernel.org/doc/Documentation/filesystems/efivarfs.txt
// for more details.
func ReadVarString(name string) (string, VariableAttr, error) {
	varf, attr, sz := mylog.Check4(openEFIVar(name))

	defer varf.Close()
	// TODO: consider using golang.org/x/text/encoding/unicode here
	if sz%2 != 0 {
		return "", 0, fmt.Errorf("EFI var %q is not a valid UTF16 string, it has an extra byte", name)
	}
	n := int(sz / 2)
	if n == 0 {
		return "", attr, nil
	}
	r16 := make([]uint16, n)
	mylog.Check(binary.Read(varf, binary.LittleEndian, r16))

	if r16[n-1] == 0 {
		n--
	}
	b := &bytes.Buffer{}
	for _, r := range utf16.Decode(r16[:n]) {
		b.WriteRune(r)
	}
	return b.String(), attr, nil
}

// MockVars mocks EFI variables as read by ReadVar*, only to be used
// from tests. Set vars to nil to mock a non-EFI system.
func MockVars(vars map[string][]byte, attrs map[string]VariableAttr) (restore func()) {
	osutil.MustBeTestBinary("MockVars only to be used from tests")
	old := openEFIVar
	openEFIVar = func(name string) (io.ReadCloser, VariableAttr, int64, error) {
		if vars == nil {
			return nil, 0, 0, ErrNoEFISystem
		}
		if val, ok := vars[name]; ok {
			attr, ok := attrs[name]
			if !ok {
				attr = VariableRuntimeAccess | VariableBootServiceAccess
			}
			return io.NopCloser(bytes.NewBuffer(val)), attr, int64(len(val)), nil
		}
		return nil, 0, 0, fmt.Errorf("EFI variable %s not mocked", name)
	}

	return func() {
		openEFIVar = old
	}
}
