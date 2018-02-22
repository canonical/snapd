// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2017-2018 Canonical Ltd
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

package testutil

import (
	"fmt"
	"os"
	"strings"
	"syscall"
	"time"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/osutil/sys"
)

const UMOUNT_NOFOLLOW = 8

// fakeFileInfo implements os.FileInfo for testing.
//
// Some of the functions panic as we don't expect them to be called.
// Feel free to expand them as necessary.
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

func FakeFileInfo(name string, mode os.FileMode) os.FileInfo {
	return &fakeFileInfo{name: name, mode: mode}
}

// Convenient FakeFileInfo objects for InsertLstatResult
var (
	FileInfoFile    = &fakeFileInfo{}
	FileInfoDir     = &fakeFileInfo{mode: os.ModeDir}
	FileInfoSymlink = &fakeFileInfo{mode: os.ModeSymlink}
)

// Formatter for flags passed to open syscall.
//
// Not all flags are handled. Unknown flags cause a panic.
// Please expand the set of recognized flags as tests require.
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
//
// Not all flags are handled. Unknown flags cause a panic.
// Please expand the set of recognized flags as tests require.
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

// Formatter for flags passed to unmount syscall.
//
// Not all flags are handled. Unknown flags cause a panic.
// Please expand the set of recognized flags as tests require.
func formatUnmountFlags(flags int) string {
	var fl []string
	if flags&UMOUNT_NOFOLLOW == UMOUNT_NOFOLLOW {
		flags ^= UMOUNT_NOFOLLOW
		fl = append(fl, "UMOUNT_NOFOLLOW")
	}
	if flags != 0 {
		panic(fmt.Errorf("unrecognized unmount flags %d", flags))
	}
	if len(fl) == 0 {
		return "0"
	}
	return strings.Join(fl, "|")
}

// SyscallRecorder stores which system calls were invoked.
//
// The recorder supports a small set of features useful for testing: injecting
// failures, returning pre-arranged test data, allocation, tracking and
// verification of file descriptors.
type SyscallRecorder struct {
	// History of all the system calls made.
	calls []string
	// Error function for a given system call.
	errors map[string]func() error
	// pre-arranged result of lstat, fstat and readdir calls.
	lstats   map[string]os.FileInfo
	fstats   map[string]syscall.Stat_t
	readdirs map[string][]os.FileInfo
	// allocated file descriptors
	fds map[int]string
}

// InsertFault makes given subsequent call to return the specified error.
//
// If one error is provided then the call will reliably fail that way.
// If multiple errors are given then they will be used on subsequent calls
// until the errors finally run out and the call succeeds.
func (sys *SyscallRecorder) InsertFault(call string, errors ...error) {
	if sys.errors == nil {
		sys.errors = make(map[string]func() error)
	}
	if len(errors) == 1 {
		// deterministic error
		sys.errors[call] = func() error {
			return errors[0]
		}
	} else {
		// error sequence
		sys.errors[call] = func() error {
			if len(errors) > 0 {
				err := errors[0]
				errors = errors[1:]
				return err
			}
			return nil
		}
	}
}

func (sys *SyscallRecorder) InsertFaultFunc(call string, fn func() error) {
	if sys.errors == nil {
		sys.errors = make(map[string]func() error)
	}
	sys.errors[call] = fn
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
		return fmt.Errorf("attempting to close a closed file descriptor %d", fd)
	}
	delete(sys.fds, fd)
	return nil
}

// StrayDescriptorsError returns an error if any descriptor is left unclosed.
func (sys *SyscallRecorder) StrayDescriptorsError() error {
	for fd, name := range sys.fds {
		return fmt.Errorf("unclosed file descriptor %d (%s)", fd, name)
	}
	return nil
}

func (sys *SyscallRecorder) CheckForStrayDescriptors(c *C) {
	c.Assert(sys.StrayDescriptorsError(), IsNil)
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
	if _, ok := sys.fds[dirfd]; !ok {
		sys.calls = append(sys.calls, call)
		return -1, fmt.Errorf("attempting to openat with an invalid file descriptor %d", dirfd)
	}
	if err := sys.call(call); err != nil {
		return -1, err
	}
	return sys.allocFd(call), nil
}

func (sys *SyscallRecorder) Close(fd int) error {
	if err := sys.call(fmt.Sprintf("close %d", fd)); err != nil {
		return err
	}
	return sys.freeFd(fd)
}

func (sys *SyscallRecorder) Fchown(fd int, uid sys.UserID, gid sys.GroupID) error {
	call := fmt.Sprintf("fchown %d %d %d", fd, uid, gid)
	if _, ok := sys.fds[fd]; !ok {
		sys.calls = append(sys.calls, call)
		return fmt.Errorf("attempting to fchown an invalid file descriptor %d", fd)
	}
	return sys.call(call)
}

func (sys *SyscallRecorder) Mkdirat(dirfd int, path string, mode uint32) error {
	call := fmt.Sprintf("mkdirat %d %q %#o", dirfd, path, mode)
	if _, ok := sys.fds[dirfd]; !ok {
		sys.calls = append(sys.calls, call)
		return fmt.Errorf("attempting to mkdirat with an invalid file descriptor %d", dirfd)
	}
	return sys.call(call)
}

func (sys *SyscallRecorder) Mount(source string, target string, fstype string, flags uintptr, data string) (err error) {
	return sys.call(fmt.Sprintf("mount %q %q %q %s %q", source, target, fstype, formatMountFlags(int(flags)), data))
}

func (sys *SyscallRecorder) Unmount(target string, flags int) (err error) {
	return sys.call(fmt.Sprintf("unmount %q %s", target, formatUnmountFlags(flags)))
}

// InsertLstatResult makes given subsequent call lstat return the specified fake file info.
func (sys *SyscallRecorder) InsertLstatResult(call string, fi os.FileInfo) {
	if sys.lstats == nil {
		sys.lstats = make(map[string]os.FileInfo)
	}
	sys.lstats[call] = fi
}

func (sys *SyscallRecorder) Lstat(name string) (os.FileInfo, error) {
	call := fmt.Sprintf("lstat %q", name)
	if err := sys.call(call); err != nil {
		return nil, err
	}
	if fi, ok := sys.lstats[call]; ok {
		return fi, nil
	}
	panic(fmt.Sprintf("one of InsertLstatResult() or InsertFault() for %s must be used", call))
}

// InsertFstatResult makes given subsequent call fstat return the specified stat buffer.
func (sys *SyscallRecorder) InsertFstatResult(call string, buf syscall.Stat_t) {
	if sys.fstats == nil {
		sys.fstats = make(map[string]syscall.Stat_t)
	}
	sys.fstats[call] = buf
}

func (sys *SyscallRecorder) Fstat(fd int, buf *syscall.Stat_t) error {
	call := fmt.Sprintf("fstat %d <ptr>", fd)
	if _, ok := sys.fds[fd]; !ok {
		sys.calls = append(sys.calls, call)
		return fmt.Errorf("attempting to fstat with an invalid file descriptor %d", fd)
	}
	if err := sys.call(call); err != nil {
		return err
	}
	if b, ok := sys.fstats[call]; ok {
		*buf = b
		return nil
	}
	panic(fmt.Sprintf("one of InsertFstatResult() or InsertFault() for %s must be used", call))
}

// InsertReadDirResult makes given subsequent call readdir return the specified fake file infos.
func (sys *SyscallRecorder) InsertReadDirResult(call string, infos []os.FileInfo) {
	if sys.readdirs == nil {
		sys.readdirs = make(map[string][]os.FileInfo)
	}
	sys.readdirs[call] = infos
}

func (sys *SyscallRecorder) ReadDir(dirname string) ([]os.FileInfo, error) {
	call := fmt.Sprintf("readdir %q", dirname)
	if err := sys.call(call); err != nil {
		return nil, err
	}
	if fi, ok := sys.readdirs[call]; ok {
		return fi, nil
	}
	panic(fmt.Sprintf("one of InsertReadDirResult() or InsertFault() for %s must be used", call))
}

func (sys *SyscallRecorder) Symlink(oldname, newname string) error {
	call := fmt.Sprintf("symlink %q -> %q", newname, oldname)
	return sys.call(call)
}

func (sys *SyscallRecorder) Remove(name string) error {
	call := fmt.Sprintf("remove %q", name)
	return sys.call(call)
}
