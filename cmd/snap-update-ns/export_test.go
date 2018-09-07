// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2017 Canonical Ltd
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

package main

import (
	"os"
	"syscall"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/osutil/sys"
)

var (
	// change
	ValidateInstanceName = validateInstanceName
	ProcessArguments     = processArguments
	// freezer
	FreezeSnapProcesses = freezeSnapProcesses
	ThawSnapProcesses   = thawSnapProcesses
	// utils
	PlanWritableMimic = planWritableMimic
	ExecWritableMimic = execWritableMimic

	// main
	ComputeAndSaveChanges = computeAndSaveChanges
	ApplyUserFstab        = applyUserFstab

	// bootstrap
	ClearBootstrapError = clearBootstrapError
)

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

func MockFreezerCgroupDir(c *C) (restore func()) {
	old := freezerCgroupDir
	freezerCgroupDir = c.MkDir()
	return func() {
		freezerCgroupDir = old
	}
}

func FreezerCgroupDir() string {
	return freezerCgroupDir
}

func MockChangePerform(f func(chg *Change, as *Assumptions) ([]*Change, error)) func() {
	origChangePerform := changePerform
	changePerform = f
	return func() {
		changePerform = origChangePerform
	}
}

func MockReadDir(fn func(string) ([]os.FileInfo, error)) (restore func()) {
	old := ioutilReadDir
	ioutilReadDir = fn
	return func() {
		ioutilReadDir = old
	}
}

func MockReadlink(fn func(string) (string, error)) (restore func()) {
	old := osReadlink
	osReadlink = fn
	return func() {
		osReadlink = old
	}
}
