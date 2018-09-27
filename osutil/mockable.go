// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2018 Canonical Ltd
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
	"io/ioutil"
	"os"
	"os/user"
	"syscall"

	"github.com/snapcore/snapd/osutil/sys"
)

const (
	// ProcSelfMountInfo is a path to the mountinfo table of the current process.
	ProcSelfMountInfo = "/proc/self/mountinfo"
)

// For mocking everything during testing.
var (
	userLookup  = user.Lookup
	userCurrent = user.Current

	syscallKill    = syscall.Kill
	syscallGetpgid = syscall.Getpgid

	procSelfMountInfo = ProcSelfMountInfo
	etcFstab          = "/etc/fstab"
	sudoersDotD       = "/etc/sudoers.d"

	osLstat    = os.Lstat
	osReadlink = os.Readlink
	osRemove   = os.Remove

	sysClose      = syscall.Close
	sysMkdirat    = syscall.Mkdirat
	sysMount      = syscall.Mount
	sysOpen       = syscall.Open
	sysOpenat     = syscall.Openat
	sysUnmount    = syscall.Unmount
	sysFchown     = sys.Fchown
	sysFstat      = syscall.Fstat
	sysFstatfs    = syscall.Fstatfs
	sysSymlinkat  = Symlinkat
	sysReadlinkat = Readlinkat
	sysFchdir     = syscall.Fchdir
	sysLstat      = syscall.Lstat

	ioutilReadDir = ioutil.ReadDir
)

// OsLstat is like os.Lstat but can be mocked with MockSystemCalls
func OsLstat(name string) (os.FileInfo, error) {
	return osLstat(name)
}

// OsRemove is like os.Remove but can be mocked by MockSystemCalls
func OsRemove(name string) error {
	return osRemove(name)
}

// OsReadlink is like os.ReadLink but can be mocked by MockOsReadLink
// TODO: allow mocking this with MockSystemCalls
func OsReadlink(name string) (string, error) {
	return osReadlink(name)
}

// SysLstat is like syscall.Lstat but can be mocked with MockSystemCalls
func SysLstat(name string, buf *syscall.Stat_t) error {
	return sysLstat(name, buf)
}

// SysMount is like syscall.Mount but can be mocked by MockSystemCalls
func SysMount(source string, target string, fstype string, flags uintptr, data string) error {
	return sysMount(source, target, fstype, flags, data)
}

// SysUnmount is like syscall.Umont but can be mocked by MockSystemCalls
func SysUnmount(target string, flags int) error {
	return sysUnmount(target, flags)
}

// SysFstatfs is like syscall.Fstatfs but can be mocked by MockSystemCalls
func SysFstatfs(fd int, buf *syscall.Statfs_t) error {
	return sysFstatfs(fd, buf)
}

// IoutilReadDir is like ioutil.ReadDir but can be mocked with MockReadDir
// TODO: allow mocking with MockSystemCalls
func IoutilReadDir(dirname string) ([]os.FileInfo, error) {
	return ioutilReadDir(dirname)
}

// SystemCalls encapsulates various system interactions performed by this module.
type SystemCalls interface {
	OsLstat(name string) (os.FileInfo, error)
	SysLstat(name string, buf *syscall.Stat_t) error
	ReadDir(dirname string) ([]os.FileInfo, error)
	Symlinkat(oldname string, dirfd int, newname string) error
	Readlinkat(dirfd int, path string, buf []byte) (int, error)
	Remove(name string) error

	Close(fd int) error
	Fchdir(fd int) error
	Fchown(fd int, uid sys.UserID, gid sys.GroupID) error
	Mkdirat(dirfd int, path string, mode uint32) error
	Mount(source string, target string, fstype string, flags uintptr, data string) (err error)
	Open(path string, flags int, mode uint32) (fd int, err error)
	Openat(dirfd int, path string, flags int, mode uint32) (fd int, err error)
	Unmount(target string, flags int) error
	Fstat(fd int, buf *syscall.Stat_t) error
	Fstatfs(fd int, buf *syscall.Statfs_t) error
}

// MockSystemCalls replaces real system calls with those of the argument.
func MockSystemCalls(sc SystemCalls) (restore func()) {
	// save
	oldOsLstat := osLstat
	oldRemove := osRemove
	oldIoutilReadDir := ioutilReadDir

	oldSysClose := sysClose
	oldSysFchown := sysFchown
	oldSysMkdirat := sysMkdirat
	oldSysMount := sysMount
	oldSysOpen := sysOpen
	oldSysOpenat := sysOpenat
	oldSysUnmount := sysUnmount
	oldSysSymlinkat := sysSymlinkat
	oldReadlinkat := sysReadlinkat
	oldFstat := sysFstat
	oldFstatfs := sysFstatfs
	oldSysFchdir := sysFchdir
	oldSysLstat := sysLstat

	// override
	osLstat = sc.OsLstat
	osRemove = sc.Remove
	ioutilReadDir = sc.ReadDir

	sysClose = sc.Close
	sysFchown = sc.Fchown
	sysMkdirat = sc.Mkdirat
	sysMount = sc.Mount
	sysOpen = sc.Open
	sysOpenat = sc.Openat
	sysUnmount = sc.Unmount
	sysSymlinkat = sc.Symlinkat
	sysReadlinkat = sc.Readlinkat
	sysFstat = sc.Fstat
	sysFstatfs = sc.Fstatfs
	sysFchdir = sc.Fchdir
	sysLstat = sc.SysLstat

	return func() {
		// restore
		osLstat = oldOsLstat
		osRemove = oldRemove
		ioutilReadDir = oldIoutilReadDir

		sysClose = oldSysClose
		sysFchown = oldSysFchown
		sysMkdirat = oldSysMkdirat
		sysMount = oldSysMount
		sysOpen = oldSysOpen
		sysOpenat = oldSysOpenat
		sysUnmount = oldSysUnmount
		sysSymlinkat = oldSysSymlinkat
		sysReadlinkat = oldReadlinkat
		sysFstat = oldFstat
		sysFstatfs = oldFstatfs
		sysFchdir = oldSysFchdir
		sysLstat = oldSysLstat
	}
}

// MockReadDir mocks the IoutilReadDir as provided by this package.
func MockReadDir(fn func(string) ([]os.FileInfo, error)) (restore func()) {
	old := ioutilReadDir
	ioutilReadDir = fn
	return func() {
		ioutilReadDir = old
	}
}

// MockReadlink mocks the OsReadlink as provided by this package.
func MockReadlink(fn func(string) (string, error)) (restore func()) {
	old := osReadlink
	osReadlink = fn
	return func() {
		osReadlink = old
	}
}
