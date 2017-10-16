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

	. "gopkg.in/check.v1"
)

var (
	// change
	ReadCmdline      = readCmdline
	FindSnapName     = findSnapName
	FindFirstOption  = findFirstOption
	ValidateSnapName = validateSnapName
	ProcessArguments = processArguments
	// freezer
	FreezeSnapProcesses = freezeSnapProcesses
	ThawSnapProcesses   = thawSnapProcesses
)

// fakeFileInfo implements os.FileInfo for one of the tests.
// Most of the functions panic as we don't expect them to be called.
type fakeFileInfo struct {
	mode os.FileMode
}

func (*fakeFileInfo) Name() string         { panic("unexpected call") }
func (*fakeFileInfo) Size() int64          { panic("unexpected call") }
func (fi *fakeFileInfo) Mode() os.FileMode { return fi.mode }
func (*fakeFileInfo) ModTime() time.Time   { panic("unexpected call") }
func (fi *fakeFileInfo) IsDir() bool       { return fi.Mode().IsDir() }
func (*fakeFileInfo) Sys() interface{}     { panic("unexpected call") }

// Fake FileInfo objects for InsertLstatResult
var (
	FileInfoFile    = &fakeFileInfo{}
	FileInfoDir     = &fakeFileInfo{mode: os.ModeDir}
	FileInfoSymlink = &fakeFileInfo{mode: os.ModeSymlink}
)

// SystemCalls encapsulates various system interactions performed by this module.
type SystemCalls interface {
	Mount(source string, target string, fstype string, flags uintptr, data string) (err error)
	Unmount(target string, flags int) (err error)
	Lstat(name string) (os.FileInfo, error)
	SecureMkdirAll(path string, perm os.FileMode, uid, gid int) error
}

// SyscallRecorder stores which system calls were invoked.
type SyscallRecorder struct {
	calls  []string
	errors map[string]error
	lstats map[string]*fakeFileInfo
}

// InsertFault makes given subsequent call to return the specified error.
func (sys *SyscallRecorder) InsertFault(call string, err error) {
	if sys.errors == nil {
		sys.errors = make(map[string]error)
	}
	sys.errors[call] = err
}

// InsertLstatResult makes given subsequent call lstat return the specified fake file info.
func (sys *SyscallRecorder) InsertLstatResult(call string, fi *fakeFileInfo) {
	if sys.lstats == nil {
		sys.lstats = make(map[string]*fakeFileInfo)
	}
	sys.lstats[call] = fi
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
	call := fmt.Sprintf("lstat %q", name)
	if err := sys.call(call); err != nil {
		return nil, err
	}
	if fi := sys.lstats[call]; fi != nil {
		return fi, nil
	}
	panic(fmt.Sprintf("one of InsertLstatResult() or InsertFault() for %q must be used", call))
}

func (sys *SyscallRecorder) SecureMkdirAll(path string, perm os.FileMode, uid, gid int) error {
	return sys.call(fmt.Sprintf("secure-mkdir-all %q %q %d %d", path, perm, uid, gid))
}

// MockSystemCalls replaces real system calls with those of the argument.
func MockSystemCalls(sc SystemCalls) (restore func()) {
	oldSysMount := sysMount
	oldSysUnmount := sysUnmount
	oldOsLstat := osLstat
	oldSecureMkdirAll := secureMkdirAll

	sysMount = sc.Mount
	sysUnmount = sc.Unmount
	osLstat = sc.Lstat
	secureMkdirAll = sc.SecureMkdirAll

	return func() {
		sysMount = oldSysMount
		sysUnmount = oldSysUnmount
		osLstat = oldOsLstat
		secureMkdirAll = oldSecureMkdirAll
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
