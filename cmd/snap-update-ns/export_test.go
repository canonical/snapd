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
	"strings"
	"syscall"
	"time"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/osutil/sys"
)

var (
	// change
	ValidateSnapName = validateSnapName
	ProcessArguments = processArguments
	// freezer
	FreezeSnapProcesses = freezeSnapProcesses
	ThawSnapProcesses   = thawSnapProcesses
	// utils
	PlanWritableMimic = planWritableMimic
	ExecWritableMimic = execWritableMimic
	SecureMkdirAll    = secureMkdirAll
	SecureMkfileAll   = secureMkfileAll
	SplitIntoSegments = splitIntoSegments

	// main
	ComputeAndSaveChanges = computeAndSaveChanges
)

// fakeFileInfo implements os.FileInfo for one of the tests.
// Most of the functions panic as we don't expect them to be called.
type fakeFileInfo struct {
	name string
	mode os.FileMode
}

func (fi *fakeFileInfo) Name() string      { return fi.name }
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

func FakeFileInfo(name string, mode os.FileMode) os.FileInfo {
	return &fakeFileInfo{name: name, mode: mode}
}

// Formatter for flags passed to open syscall.
func formatOpenFlags(flags int) string {
	var fl []string
	if flags&syscall.O_NOFOLLOW != 0 {
		flags ^= syscall.O_NOFOLLOW
		fl = append(fl, "O_NOFOLLOW")
	}
	if flags&syscall.O_CLOEXEC != 0 {
		flags ^= syscall.O_CLOEXEC
		fl = append(fl, "O_CLOEXEC")
	}
	if flags&syscall.O_DIRECTORY != 0 {
		flags ^= syscall.O_DIRECTORY
		fl = append(fl, "O_DIRECTORY")
	}
	if flags&syscall.O_RDWR != 0 {
		flags ^= syscall.O_RDWR
		fl = append(fl, "O_RDWR")
	}
	if flags&syscall.O_CREAT != 0 {
		flags ^= syscall.O_CREAT
		fl = append(fl, "O_CREAT")
	}
	if flags&syscall.O_EXCL != 0 {
		flags ^= syscall.O_EXCL
		fl = append(fl, "O_EXCL")
	}
	if flags != 0 {
		panic(fmt.Errorf("unrecognized open flags %d", flags))
	}
	if len(fl) == 0 {
		return "0"
	}
	return strings.Join(fl, "|")
}

// Formatter for flags passed to mount syscall.
func formatMountFlags(flags int) string {
	var fl []string
	if flags&syscall.MS_BIND == syscall.MS_BIND {
		flags ^= syscall.MS_BIND
		fl = append(fl, "MS_BIND")
	}
	if flags&syscall.MS_RDONLY == syscall.MS_RDONLY {
		flags ^= syscall.MS_RDONLY
		fl = append(fl, "MS_RDONLY")
	}
	if flags != 0 {
		panic(fmt.Errorf("unrecognized mount flags %d", flags))
	}
	if len(fl) == 0 {
		return "0"
	}
	return strings.Join(fl, "|")
}

// SystemCalls encapsulates various system interactions performed by this module.
type SystemCalls interface {
	Lstat(name string) (os.FileInfo, error)
	ReadDir(dirname string) ([]os.FileInfo, error)
	Symlink(oldname, newname string) error
	Remove(name string) error

	Close(fd int) error
	Fchown(fd int, uid sys.UserID, gid sys.GroupID) error
	Mkdirat(dirfd int, path string, mode uint32) error
	Mount(source string, target string, fstype string, flags uintptr, data string) (err error)
	Open(path string, flags int, mode uint32) (fd int, err error)
	Openat(dirfd int, path string, flags int, mode uint32) (fd int, err error)
	Unmount(target string, flags int) error
}

// SyscallRecorder stores which system calls were invoked.
type SyscallRecorder struct {
	calls    []string
	errors   map[string]func() error
	lstats   map[string]os.FileInfo
	readdirs map[string][]os.FileInfo
	fds      map[int]string
}

// InsertFault makes given subsequent call to return the specified error.
func (sys *SyscallRecorder) InsertFault(call string, err error) {
	if sys.errors == nil {
		sys.errors = make(map[string]func() error)
	}
	sys.errors[call] = func() error { return err }
}

func (sys *SyscallRecorder) InsertFaultFunc(call string, fn func() error) {
	if sys.errors == nil {
		sys.errors = make(map[string]func() error)
	}
	sys.errors[call] = fn
}

// InsertLstatResult makes given subsequent call lstat return the specified fake file info.
func (sys *SyscallRecorder) InsertLstatResult(call string, fi os.FileInfo) {
	if sys.lstats == nil {
		sys.lstats = make(map[string]os.FileInfo)
	}
	sys.lstats[call] = fi
}

// InsertReadDirResult makes given subsequent call readdir return the specified fake file infos.
func (sys *SyscallRecorder) InsertReadDirResult(call string, infos []os.FileInfo) {
	if sys.readdirs == nil {
		sys.readdirs = make(map[string][]os.FileInfo)
	}
	sys.readdirs[call] = infos
}

// Calls returns the sequence of mocked calls that have been made.
func (sys *SyscallRecorder) Calls() []string {
	return sys.calls
}

// call remembers that a given call has occurred and returns a pre-arranged error, if any
func (sys *SyscallRecorder) call(call string) error {
	sys.calls = append(sys.calls, call)
	if fn := sys.errors[call]; fn != nil {
		return fn()
	}
	return nil
}

// allocFd assigns a file descriptor to a given operation.
func (sys *SyscallRecorder) allocFd(name string) int {
	if sys.fds == nil {
		sys.fds = make(map[int]string)
	}

	// Use 3 as the lowest number for tests to look more plausible.
	for i := 3; i < 100; i++ {
		if _, ok := sys.fds[i]; !ok {
			sys.fds[i] = name
			return i
		}
	}
	panic("cannot find unused file descriptor")
}

// freeFd closes an open file descriptor.
func (sys *SyscallRecorder) freeFd(fd int) error {
	if _, ok := sys.fds[fd]; !ok {
		return fmt.Errorf("attempting to close closed file descriptor %d", fd)
	}
	delete(sys.fds, fd)
	return nil
}

func (sys *SyscallRecorder) CheckForStrayDescriptors(c *C) {
	for fd, ok := range sys.fds {
		c.Assert(ok, Equals, false, Commentf("unclosed file descriptor %d", fd))
	}
}

func (sys *SyscallRecorder) Close(fd int) error {
	if err := sys.call(fmt.Sprintf("close %d", fd)); err != nil {
		return err
	}
	return sys.freeFd(fd)
}

func (sys *SyscallRecorder) Fchown(fd int, uid sys.UserID, gid sys.GroupID) error {
	return sys.call(fmt.Sprintf("fchown %d %d %d", fd, uid, gid))
}

func (sys *SyscallRecorder) Mkdirat(dirfd int, path string, mode uint32) error {
	return sys.call(fmt.Sprintf("mkdirat %d %q %#o", dirfd, path, mode))
}

func (sys *SyscallRecorder) Open(path string, flags int, mode uint32) (int, error) {
	call := fmt.Sprintf("open %q %s %#o", path, formatOpenFlags(flags), mode)
	if err := sys.call(call); err != nil {
		return -1, err
	}
	return sys.allocFd(call), nil
}

func (sys *SyscallRecorder) Openat(dirfd int, path string, flags int, mode uint32) (int, error) {
	call := fmt.Sprintf("openat %d %q %s %#o", dirfd, path, formatOpenFlags(flags), mode)
	if err := sys.call(call); err != nil {
		return -1, err
	}
	return sys.allocFd(call), nil
}

func (sys *SyscallRecorder) Mount(source string, target string, fstype string, flags uintptr, data string) (err error) {
	return sys.call(fmt.Sprintf("mount %q %q %q %s %q", source, target, fstype, formatMountFlags(int(flags)), data))
}

func (sys *SyscallRecorder) Unmount(target string, flags int) (err error) {
	if flags == umountNoFollow {
		return sys.call(fmt.Sprintf("unmount %q %s", target, "UMOUNT_NOFOLLOW"))
	}
	return sys.call(fmt.Sprintf("unmount %q %d", target, flags))
}

func (sys *SyscallRecorder) Lstat(name string) (os.FileInfo, error) {
	call := fmt.Sprintf("lstat %q", name)
	if err := sys.call(call); err != nil {
		return nil, err
	}
	if fi, ok := sys.lstats[call]; ok {
		return fi, nil
	}
	panic(fmt.Sprintf("one of InsertLstatResult() or InsertFault() for %q must be used", call))
}

func (sys *SyscallRecorder) ReadDir(dirname string) ([]os.FileInfo, error) {
	call := fmt.Sprintf("readdir %q", dirname)
	if err := sys.call(call); err != nil {
		return nil, err
	}
	if fi, ok := sys.readdirs[call]; ok {
		return fi, nil
	}
	panic(fmt.Sprintf("one of InsertReadDirResult() or InsertFault() for %q must be used", call))
}

func (sys *SyscallRecorder) Symlink(oldname, newname string) error {
	call := fmt.Sprintf("symlink %q -> %q", newname, oldname)
	return sys.call(call)
}

func (sys *SyscallRecorder) Remove(name string) error {
	call := fmt.Sprintf("remove %q", name)
	return sys.call(call)
}

// MockSystemCalls replaces real system calls with those of the argument.
func MockSystemCalls(sc SystemCalls) (restore func()) {
	// save
	oldOsLstat := osLstat
	oldSymlink := osSymlink
	oldRemove := osRemove
	oldIoutilReadDir := ioutilReadDir

	oldSysClose := sysClose
	oldSysFchown := sysFchown
	oldSysMkdirat := sysMkdirat
	oldSysMount := sysMount
	oldSysOpen := sysOpen
	oldSysOpenat := sysOpenat
	oldSysUnmount := sysUnmount

	// override
	osLstat = sc.Lstat
	osSymlink = sc.Symlink
	osRemove = sc.Remove
	ioutilReadDir = sc.ReadDir

	sysClose = sc.Close
	sysFchown = sc.Fchown
	sysMkdirat = sc.Mkdirat
	sysMount = sc.Mount
	sysOpen = sc.Open
	sysOpenat = sc.Openat
	sysUnmount = sc.Unmount

	return func() {
		// restore
		osLstat = oldOsLstat
		osSymlink = oldSymlink
		osRemove = oldRemove
		ioutilReadDir = oldIoutilReadDir

		sysClose = oldSysClose
		sysFchown = oldSysFchown
		sysMkdirat = oldSysMkdirat
		sysMount = oldSysMount
		sysOpen = oldSysOpen
		sysOpenat = oldSysOpenat
		sysUnmount = oldSysUnmount
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

func MockChangePerform(f func(chg *Change) ([]*Change, error)) func() {
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
