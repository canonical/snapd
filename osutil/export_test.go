// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2016 Canonical Ltd
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

package osutil

import (
	"os"
	"os/exec"
	"os/user"
)

func MockUserLookup(mock func(name string) (*user.User, error)) func() {
	realUserLookup := userLookup
	userLookup = mock

	return func() { userLookup = realUserLookup }
}

func MockUserCurrent(mock func() (*user.User, error)) func() {
	realUserCurrent := userCurrent
	userCurrent = mock

	return func() { userCurrent = realUserCurrent }
}

func MockSudoersDotD(mockDir string) func() {
	realSudoersD := sudoersDotD
	sudoersDotD = mockDir

	return func() { sudoersDotD = realSudoersD }
}

func MockMountInfoPath(mockMountInfoPath string) func() {
	realMountInfoPath := mountInfoPath
	mountInfoPath = mockMountInfoPath

	return func() { mountInfoPath = realMountInfoPath }
}

func MockCmpBufSize(newBufsz int) func() {
	bufsz = newBufsz
	return func() { bufsz = defaultBufsz }
}

func MockOpenfile(mock func(name string, flag int, perm os.FileMode) (Fileish, error)) func() {
	openfile = mock
	return func() { openfile = doOpenFile }
}

func MockCopyfile(mock func(fin, fout Fileish, fi os.FileInfo) error) func() {
	copyfile = mock
	return func() { copyfile = doCopyFile }
}

func MockMaxCp(newMax int64) func() {
	maxcp = newMax
	return func() { maxcp = maxint }
}

var DoCopyFile = doCopyFile

func MockLookPath(mock func(name string) (string, error)) func() {
	lookPath = mock
	return func() { lookPath = exec.LookPath }
}
