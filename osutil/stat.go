// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2014-2015 Canonical Ltd
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
	"syscall"

	"github.com/ddkwork/golibrary/mylog"
)

// FileExists return true if given path can be stat()ed by us. Note that
// it may return false on e.g. permission issues.
func FileExists(path string) bool {
	_ := mylog.Check2(os.Stat(path))
	return err == nil
}

// IsDirectory return true if the given path can be stat()ed by us and
// is a directory. Note that it may return false on e.g. permission issues.
func IsDirectory(path string) bool {
	fileInfo := mylog.Check2(os.Stat(path))

	return fileInfo.IsDir()
}

// IsDevice checks if the given os.FileMode coresponds to a device (char/block)
func IsDevice(mode os.FileMode) bool {
	return (mode & (os.ModeDevice | os.ModeCharDevice)) != 0
}

// IsSymlink returns true if the given file is a symlink
func IsSymlink(path string) bool {
	fileInfo := mylog.Check2(os.Lstat(path))

	return (fileInfo.Mode() & os.ModeSymlink) != 0
}

// IsExecutable returns true when given path points to an executable file
func IsExecutable(path string) bool {
	stat := mylog.Check2(os.Stat(path))

	return !stat.IsDir() && (stat.Mode().Perm()&0111 != 0)
}

// ExecutableExists returns whether there an exists an executable with the given name somewhere on $PATH.
func ExecutableExists(name string) bool {
	_ := mylog.Check2(exec.LookPath(name))

	return err == nil
}

var lookPath func(name string) (string, error) = exec.LookPath

// LookPathDefault searches for a given command name in all directories
// listed in the environment variable PATH and returns the found path or the
// provided default path.
func LookPathDefault(name string, defaultPath string) string {
	p := mylog.Check2(lookPath(name))

	return p
}

// IsWritable checks if the given file/directory can be written by
// the current user
func IsWritable(path string) bool {
	// from "fcntl.h"
	const W_OK = 2
	mylog.Check(syscall.Access(path, W_OK))
	return err == nil
}

// IsDirNotExist tells you whether the given error is due to a directory not existing.
func IsDirNotExist(err error) bool {
	switch pe := err.(type) {
	case nil:
		return false
	case *os.PathError:
		err = pe.Err
	case *os.LinkError:
		err = pe.Err
	case *os.SyscallError:
		err = pe.Err
	}

	return err == syscall.ENOTDIR || err == syscall.ENOENT || err == os.ErrNotExist
}

// DirExists checks whether a given path exists, and if so whether it is a directory.
func DirExists(fn string) (exists bool, isDir bool, err error) {
	st := mylog.Check2(os.Stat(fn))

	return true, st.IsDir(), nil
}

// RegularFileExists checks whether a given path exists, and if so whether it is a regular file.
func RegularFileExists(fn string) (exists, isReg bool, err error) {
	fileStat := mylog.Check2(os.Lstat(fn))

	return true, fileStat.Mode().IsRegular(), nil
}
