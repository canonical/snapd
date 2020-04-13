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

package efi

import (
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"strings"

	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/osutil"
)

const (
	defaultEfiVarfsDir    = "/sys/firmware/efivars"
	defaultEfiVarSysfsDir = "/sys/firmware/efi/vars"
)

var (
	readEfiVar  = readEfiVarImpl
	isSnapdTest = len(os.Args) > 0 && strings.HasSuffix(os.Args[0], ".test")
)

// ReadEfiVar will attempt to read the binary value of the specified efi
// variable, specified by it's full name of the variable and vendor ID.
// It first tries to read from efivarfs wherever that is mounted, and falls back
// to sysfs if that is unavailable. See
// https://www.kernel.org/doc/Documentation/filesystems/efivarfs.txt for more
// details.
func ReadEfiVar(name string) ([]byte, error) {
	return readEfiVar(name)
}

func readEfiVarImpl(name string) ([]byte, error) {
	// check if we have the efivars fs mounted first, if so then use that
	// for reading the efi var
	efiVarDir := filepath.Join(dirs.GlobalRootDir, defaultEfiVarfsDir)
	fallbackToSysfs := false
	mounts, err := osutil.LoadMountInfo()
	if err == nil {
		found := false
		for _, mnt := range mounts {
			if mnt.FsType == "efivarfs" {
				// use this mount point as an absolute point - i.e. don't prefix
				// with GlobalRootDir
				efiVarDir = mnt.MountDir
				found = true
				break
			}
		}
		if !found {
			// we have procfs mounted, but no efivarfs, so fallback to trying
			// sysfs instead
			fallbackToSysfs = true
		}
	} else {
		// could be we have efivarfs mounted, but procfs is missing for some
		// reason so we couldn't read /proc

		// if the efiVarDir exists, then efivarfs is mounted, we just don't know
		// that because we couldn't read procfs
		if !osutil.FileExists(efiVarDir) {
			fallbackToSysfs = true
		}
	}

	varFilePath := filepath.Join(efiVarDir, name)
	if fallbackToSysfs {
		// the data file is only used when reading from the sysfs endpoint
		varFilePath = filepath.Join(dirs.GlobalRootDir, defaultEfiVarSysfsDir, name, "data")
	}
	return ioutil.ReadFile(varFilePath)

}

// MockEfiVariables mocks efi variables as read by ReadEfiVar, only to be used
// from tests.
func MockEfiVariables(vars map[string][]byte) (restore func()) {
	if !isSnapdTest {
		panic("MockEfiVariables only to be used from tests")
	}
	old := readEfiVar
	readEfiVar = func(name string) ([]byte, error) {
		if val, ok := vars[name]; ok {
			return val, nil
		}
		return nil, fmt.Errorf("efi variable %s not mocked", name)
	}

	return func() {
		readEfiVar = old
	}
}
