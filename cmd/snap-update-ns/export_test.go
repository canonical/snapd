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
	"fmt"
	"os"
	"time"
)

var (
	ReadCmdline      = readCmdline
	FindSnapName     = findSnapName
	FindFirstOption  = findFirstOption
	ValidateSnapName = validateSnapName
	ProcessArguments = processArguments
)

// fakeFileInfo implements os.FileInfo for one of the tests.
// All of the functions panic as we don't expect them to be called.
type fakeFileInfo struct{}

func (*fakeFileInfo) Name() string       { panic("unexpected call") }
func (*fakeFileInfo) Size() int64        { panic("unexpected call") }
func (*fakeFileInfo) Mode() os.FileMode  { panic("unexpected call") }
func (*fakeFileInfo) ModTime() time.Time { panic("unexpected call") }
func (*fakeFileInfo) IsDir() bool        { panic("unexpected call") }
func (*fakeFileInfo) Sys() interface{}   { panic("unexpected call") }

// SystemCalls encapsulates various system interactions performed by this module.
type SystemCalls interface {
	Mount(source string, target string, fstype string, flags uintptr, data string) (err error)
	Unmount(target string, flags int) (err error)
	Lstat(name string) (os.FileInfo, error)
	MkdirAll(path string, perm os.FileMode) error
	Chown(name string, uid, gid int) error
}

// SyscallRecorder stores which system calls were invoked.
type SyscallRecorder struct {
	calls  []string
	errors map[string]error
}

// InsertFault makes given subsequent call to return the specified error.
func (sys *SyscallRecorder) InsertFault(call string, err error) {
	if sys.errors == nil {
		sys.errors = make(map[string]error)
	}
	sys.errors[call] = err
}

// Calls returns the sequence of mocked calls that have been made.
func (sys *SyscallRecorder) Calls() []string {
	return sys.calls
}

// call remembers that a given call has occurred and returns a pre-arranged error, if any
func (sys *SyscallRecorder) call(call string) error {
	sys.calls = append(sys.calls, call)
	return sys.errors[call]
}

func (sys *SyscallRecorder) Mount(source string, target string, fstype string, flags uintptr, data string) (err error) {
	return sys.call(fmt.Sprintf("mount %q %q %q %d %q", source, target, fstype, flags, data))
}

func (sys *SyscallRecorder) Unmount(target string, flags int) (err error) {
	if flags == unmountNoFollow {
		return sys.call(fmt.Sprintf("unmount %q %s", target, "UMOUNT_NOFOLLOW"))
	}
	return sys.call(fmt.Sprintf("unmount %q %d", target, flags))
}

func (sys *SyscallRecorder) Lstat(name string) (os.FileInfo, error) {
	err := sys.call(fmt.Sprintf("lstat %q", name))
	return &fakeFileInfo{}, err
}

func (sys *SyscallRecorder) MkdirAll(path string, perm os.FileMode) error {
	return sys.call(fmt.Sprintf("mkdirall %q %q", path, perm))
}

func (sys *SyscallRecorder) Chown(name string, uid, gid int) error {
	return sys.call(fmt.Sprintf("chown %q %d %d", name, uid, gid))
}

// MockSystemCalls replaces real system calls with those of the argument.
func MockSystemCalls(sc SystemCalls) (restore func()) {
	oldSysMount := sysMount
	oldSysUnmount := sysUnmount
	oldOsLstat := osLstat
	oldOsMkdirAll := osMkdirAll
	oldOsChown := osChown

	sysMount = sc.Mount
	sysUnmount = sc.Unmount
	osLstat = sc.Lstat
	osMkdirAll = sc.MkdirAll
	osChown = sc.Chown

	return func() {
		sysMount = oldSysMount
		sysUnmount = oldSysUnmount
		osLstat = oldOsLstat
		osMkdirAll = oldOsMkdirAll
		osChown = oldOsChown
	}
}
