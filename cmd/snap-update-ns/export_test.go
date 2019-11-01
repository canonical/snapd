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

	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/osutil/sys"
)

var (
	// change
	ValidateInstanceName = validateInstanceName
	ProcessArguments     = processArguments

	// utils
	PlanWritableMimic = planWritableMimic
	ExecWritableMimic = execWritableMimic

	// bootstrap
	ClearBootstrapError = clearBootstrapError

	// trespassing
	IsReadOnly                   = isReadOnly
	IsPrivateTmpfsCreatedBySnapd = isPrivateTmpfsCreatedBySnapd

	// system
	DesiredSystemProfilePath = desiredSystemProfilePath
	CurrentSystemProfilePath = currentSystemProfilePath

	// user
	DesiredUserProfilePath = desiredUserProfilePath
	CurrentUserProfilePath = currentUserProfilePath

	// xdg
	XdgRuntimeDir        = xdgRuntimeDir
	ExpandPrefixVariable = expandPrefixVariable
	ExpandXdgRuntimeDir  = expandXdgRuntimeDir

	// update
	ExecuteMountProfileUpdate = executeMountProfileUpdate
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

func MockChangePerform(f func(chg *Change, as *Assumptions) ([]*Change, error)) func() {
	origChangePerform := changePerform
	changePerform = f
	return func() {
		changePerform = origChangePerform
	}
}

func MockNeededChanges(f func(old, new *osutil.MountProfile) []*Change) (restore func()) {
	origNeededChanges := NeededChanges
	NeededChanges = f
	return func() {
		NeededChanges = origNeededChanges
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

func (as *Assumptions) IsRestricted(path string) bool {
	return as.isRestricted(path)
}

func (as *Assumptions) PastChanges() []*Change {
	return as.pastChanges
}

func (as *Assumptions) CanWriteToDirectory(dirFd int, dirName string) (bool, error) {
	return as.canWriteToDirectory(dirFd, dirName)
}

func (as *Assumptions) UnrestrictedPaths() []string {
	return as.unrestrictedPaths
}

func (upCtx *CommonProfileUpdateContext) CurrentProfilePath() string {
	return upCtx.currentProfilePath
}

func (upCtx *CommonProfileUpdateContext) DesiredProfilePath() string {
	return upCtx.desiredProfilePath
}

func (upCtx *CommonProfileUpdateContext) FromSnapConfine() bool {
	return upCtx.fromSnapConfine
}

func (upCtx *CommonProfileUpdateContext) SetFromSnapConfine(v bool) {
	upCtx.fromSnapConfine = v
}

func NewCommonProfileUpdateContext(instanceName string, fromSnapConfine bool, currentProfilePath, desiredProfilePath string) *CommonProfileUpdateContext {
	return &CommonProfileUpdateContext{
		instanceName:       instanceName,
		fromSnapConfine:    fromSnapConfine,
		currentProfilePath: currentProfilePath,
		desiredProfilePath: desiredProfilePath,
	}
}
