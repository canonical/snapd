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
	"io/fs"
	"os"
	"strings"
	"syscall"
	"time"

	"gopkg.in/check.v1"

	"github.com/snapcore/snapd/osutil/mount"
	"github.com/snapcore/snapd/osutil/sys"
)

const umountNoFollow = 8

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

// FakeFileInfo returns a fake object implementing os.FileInfo
func FakeFileInfo(name string, mode os.FileMode) os.FileInfo {
	return &fakeFileInfo{name: name, mode: mode}
}

// fakeDirEntry implements fs.DirEntry for testing.
type fakeDirEntry struct {
	name string
	mode os.FileMode
}

func (de *fakeDirEntry) Name() string      { return de.name }
func (de *fakeDirEntry) Type() fs.FileMode { return de.mode.Type() }
func (de *fakeDirEntry) IsDir() bool       { return de.Type().IsDir() }
func (de *fakeDirEntry) Info() (fs.FileInfo, error) {
	return &fakeFileInfo{
		name: de.Name(),
		mode: de.mode,
	}, nil
}

// FakeDirEntry returns a fake object implementing fs.DirEntry
func FakeDirEntry(name string, mode os.FileMode) fs.DirEntry {
	return &fakeDirEntry{name: name, mode: mode}
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
	if flags&sys.O_PATH != 0 {
		flags ^= sys.O_PATH
		fl = append(fl, "O_PATH")
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
	fl, unknown := mount.MountFlagsToOpts(flags)
	if unknown != 0 {
		panic(fmt.Errorf("unrecognized mount flags %d", unknown))
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
	fl, unknown := mount.UnmountFlagsToOpts(flags)
	if unknown != 0 {
		panic(fmt.Errorf("unrecognized unmount flags %d", unknown))
	}
	if len(fl) == 0 {
		return "0"
	}
	return strings.Join(fl, "|")
}

// CallResultError describes a system call and the corresponding result or error.
//
// The field names stand for Call, Result and Error respectively. They are
// abbreviated due to the nature of their use (in large quantity).
type CallResultError struct {
	C string
	R interface{}
	E error
}

// SyscallRecorder stores which system calls were invoked.
//
// The recorder supports a small set of features useful for testing: injecting
// failures, returning pre-arranged test data, allocation, tracking and
// verification of file descriptors.
type SyscallRecorder struct {
	// History of all the system calls made.
	rcalls []CallResultError
	// Error function for a given system call.
	errors map[string]func() error
	// pre-arranged result of lstat, fstat and readdir calls.
	osLstats    map[string]os.FileInfo
	sysLstats   map[string]syscall.Stat_t
	fstats      map[string]syscall.Stat_t
	fstatfses   map[string]func() syscall.Statfs_t
	readdirs    map[string][]fs.DirEntry
	readlinkats map[string]string
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

// InsertFaultFunc arranges given function to be called whenever given call is made.
//
// The main purpose is to allow to vary the behavior of a given system call over time.
// The provided function can return an error or nil to indicate success.
func (sys *SyscallRecorder) InsertFaultFunc(call string, fn func() error) {
	if sys.errors == nil {
		sys.errors = make(map[string]func() error)
	}
	sys.errors[call] = fn
}

// Calls returns the sequence of mocked calls that have been made.
func (sys *SyscallRecorder) Calls() []string {
	if len(sys.rcalls) == 0 {
		return nil
	}
	calls := make([]string, 0, len(sys.rcalls))
	for _, rc := range sys.rcalls {
		calls = append(calls, rc.C)
	}
	return calls
}

// RCalls returns the sequence of mocked calls that have been made along with their results.
func (sys *SyscallRecorder) RCalls() []CallResultError {
	return sys.rcalls
}

// rcall remembers that a given call has occurred and returns a pre-arranged error or value, if any
func (sys *SyscallRecorder) rcall(call string, resultFn func(call string) (interface{}, error)) (val interface{}, err error) {
	if errorFn := sys.errors[call]; errorFn != nil {
		err = errorFn()
	}
	if err == nil && resultFn != nil {
		val, err = resultFn(call)
	}
	if err != nil {
		sys.rcalls = append(sys.rcalls, CallResultError{C: call, E: err})
	} else {
		sys.rcalls = append(sys.rcalls, CallResultError{C: call, R: val})
	}
	return val, err
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

// CheckForStrayDescriptors ensures that all fake file descriptors are closed.
func (sys *SyscallRecorder) CheckForStrayDescriptors(c *check.C) {
	c.Assert(sys.StrayDescriptorsError(), check.IsNil)
}

// Open is a fake implementation of syscall.Open
func (sys *SyscallRecorder) Open(path string, flags int, mode uint32) (int, error) {
	call := fmt.Sprintf("open %q %s %#o", path, formatOpenFlags(flags), mode)
	fd, err := sys.rcall(call, func(call string) (interface{}, error) {
		return sys.allocFd(call), nil
	})
	if err != nil {
		return -1, err
	}
	return fd.(int), nil
}

// Openat is a fake implementation of syscall.Openat
func (sys *SyscallRecorder) Openat(dirfd int, path string, flags int, mode uint32) (int, error) {
	call := fmt.Sprintf("openat %d %q %s %#o", dirfd, path, formatOpenFlags(flags), mode)
	fd, err := sys.rcall(call, func(call string) (interface{}, error) {
		if _, ok := sys.fds[dirfd]; !ok {
			return -1, fmt.Errorf("attempting to openat with an invalid file descriptor %d", dirfd)
		}
		return sys.allocFd(call), nil
	})
	if err != nil {
		return -1, err
	}
	return fd.(int), nil
}

// Close is a fake implementation of syscall.Close
func (sys *SyscallRecorder) Close(fd int) error {
	call := fmt.Sprintf("close %d", fd)
	_, err := sys.rcall(call, func(call string) (interface{}, error) {
		return nil, sys.freeFd(fd)
	})
	return err
}

// Fchown is a fake implementation of syscall.Fchown
func (sys *SyscallRecorder) Fchown(fd int, uid sys.UserID, gid sys.GroupID) error {
	call := fmt.Sprintf("fchown %d %d %d", fd, uid, gid)
	_, err := sys.rcall(call, func(call string) (interface{}, error) {
		if _, ok := sys.fds[fd]; !ok {
			return nil, fmt.Errorf("attempting to fchown an invalid file descriptor %d", fd)
		}
		return nil, nil
	})
	return err
}

// Mkdirat is a fake implementation of syscall.Mkdirat
func (sys *SyscallRecorder) Mkdirat(dirfd int, path string, mode uint32) error {
	call := fmt.Sprintf("mkdirat %d %q %#o", dirfd, path, mode)
	_, err := sys.rcall(call, func(call string) (interface{}, error) {
		if _, ok := sys.fds[dirfd]; !ok {
			return nil, fmt.Errorf("attempting to mkdirat with an invalid file descriptor %d", dirfd)
		}
		return nil, nil
	})
	return err
}

// Mount is a fake implementation of syscall.Mount
func (sys *SyscallRecorder) Mount(source string, target string, fstype string, flags uintptr, data string) error {
	call := fmt.Sprintf("mount %q %q %q %s %q", source, target, fstype, formatMountFlags(int(flags)), data)
	_, err := sys.rcall(call, nil)
	return err
}

// Unmount is a fake implementation of syscall.Unmount
func (sys *SyscallRecorder) Unmount(target string, flags int) error {
	call := fmt.Sprintf("unmount %q %s", target, formatUnmountFlags(flags))
	_, err := sys.rcall(call, nil)
	return err
}

// InsertOsLstatResult makes given subsequent call to OsLstat return the specified fake file info.
func (sys *SyscallRecorder) InsertOsLstatResult(call string, fi os.FileInfo) {
	if sys.osLstats == nil {
		sys.osLstats = make(map[string]os.FileInfo)
	}
	sys.osLstats[call] = fi
}

// InsertSysLstatResult makes given subsequent call to SysLstat return the specified fake file info.
func (sys *SyscallRecorder) InsertSysLstatResult(call string, sb syscall.Stat_t) {
	if sys.sysLstats == nil {
		sys.sysLstats = make(map[string]syscall.Stat_t)
	}
	sys.sysLstats[call] = sb
}

// OsLstat is a fake implementation of os.Lstat
func (sys *SyscallRecorder) OsLstat(name string) (os.FileInfo, error) {
	// NOTE the syscall.Lstat uses a different signature `lstat %q <ptr>`.
	call := fmt.Sprintf("lstat %q", name)
	val, err := sys.rcall(call, func(call string) (interface{}, error) {
		if fi, ok := sys.osLstats[call]; ok {
			return fi, nil
		}
		panic(fmt.Sprintf("one of InsertOsLstatResult() or InsertFault() for %s must be used", call))
	})
	if err != nil {
		return nil, err
	}
	return val.(os.FileInfo), err
}

// SysLstat is a fake implementation of syscall.Lstat
func (sys *SyscallRecorder) SysLstat(name string, sb *syscall.Stat_t) error {
	// NOTE the os.Lstat uses a different signature `lstat %q`.
	call := fmt.Sprintf("lstat %q <ptr>", name)
	val, err := sys.rcall(call, func(call string) (interface{}, error) {
		if buf, ok := sys.sysLstats[call]; ok {
			return buf, nil
		}
		panic(fmt.Sprintf("one of InsertSysLstatResult() or InsertFault() for %s must be used", call))
	})
	if err == nil && sb != nil {
		*sb = val.(syscall.Stat_t)
	}
	return err
}

// InsertFstatResult makes given subsequent call fstat return the specified stat buffer.
func (sys *SyscallRecorder) InsertFstatResult(call string, buf syscall.Stat_t) {
	if sys.fstats == nil {
		sys.fstats = make(map[string]syscall.Stat_t)
	}
	sys.fstats[call] = buf
}

// Fstat is a fake implementation of syscall.Fstat
func (sys *SyscallRecorder) Fstat(fd int, buf *syscall.Stat_t) error {
	call := fmt.Sprintf("fstat %d <ptr>", fd)
	val, err := sys.rcall(call, func(call string) (interface{}, error) {
		if _, ok := sys.fds[fd]; !ok {
			return nil, fmt.Errorf("attempting to fstat with an invalid file descriptor %d", fd)
		}
		if buf, ok := sys.fstats[call]; ok {
			return buf, nil
		}
		panic(fmt.Sprintf("one of InsertFstatResult() or InsertFault() for %s must be used", call))
	})
	if err == nil && buf != nil {
		*buf = val.(syscall.Stat_t)
	}
	return err
}

// InsertFstatfsResult makes given subsequent call fstatfs return the specified stat buffer.
func (sys *SyscallRecorder) InsertFstatfsResult(call string, bufs ...syscall.Statfs_t) {
	if sys.fstatfses == nil {
		sys.fstatfses = make(map[string]func() syscall.Statfs_t)
	}
	if len(bufs) == 0 {
		panic("cannot provide zero results to InsertFstatfsResult")
	}
	if len(bufs) == 1 {
		// deterministic behavior
		sys.fstatfses[call] = func() syscall.Statfs_t {
			return bufs[0]
		}
	} else {
		// sequential results with the last element repeated forever.
		sys.fstatfses[call] = func() syscall.Statfs_t {
			buf := bufs[0]
			if len(bufs) > 1 {
				bufs = bufs[1:]
			}
			return buf
		}
	}
}

// Fstatfs is a fake implementation of syscall.Fstatfs
func (sys *SyscallRecorder) Fstatfs(fd int, buf *syscall.Statfs_t) error {
	call := fmt.Sprintf("fstatfs %d <ptr>", fd)
	val, err := sys.rcall(call, func(call string) (interface{}, error) {
		if _, ok := sys.fds[fd]; !ok {
			return nil, fmt.Errorf("attempting to fstatfs with an invalid file descriptor %d", fd)
		}
		if bufFn, ok := sys.fstatfses[call]; ok {
			return bufFn(), nil
		}
		panic(fmt.Sprintf("one of InsertFstatfsResult() or InsertFault() for %s must be used", call))
	})
	if err == nil && buf != nil {
		*buf = val.(syscall.Statfs_t)
	}
	return err
}

// InsertReadDirResult makes given subsequent call readdir return the specified fake file infos.
func (sys *SyscallRecorder) InsertReadDirResult(call string, infos []fs.DirEntry) {
	if sys.readdirs == nil {
		sys.readdirs = make(map[string][]fs.DirEntry)
	}
	sys.readdirs[call] = infos
}

// ReadDir is a fake implementation of os.ReadDir
func (sys *SyscallRecorder) ReadDir(dirname string) ([]fs.DirEntry, error) {
	call := fmt.Sprintf("readdir %q", dirname)
	val, err := sys.rcall(call, func(call string) (interface{}, error) {
		if fi, ok := sys.readdirs[call]; ok {
			return fi, nil
		}
		panic(fmt.Sprintf("one of InsertReadDirResult() or InsertFault() for %s must be used", call))
	})
	if err == nil {
		return val.([]fs.DirEntry), nil
	}
	return nil, err
}

// Symlink is a fake implementation of syscall.Symlink
func (sys *SyscallRecorder) Symlink(oldname, newname string) error {
	call := fmt.Sprintf("symlink %q -> %q", newname, oldname)
	_, err := sys.rcall(call, nil)
	return err
}

// Symlinkat is a fake implementation of osutil.Symlinkat (syscall.Symlinkat is not exposed)
func (sys *SyscallRecorder) Symlinkat(oldname string, dirfd int, newname string) error {
	call := fmt.Sprintf("symlinkat %q %d %q", oldname, dirfd, newname)
	_, err := sys.rcall(call, func(call string) (interface{}, error) {
		if _, ok := sys.fds[dirfd]; !ok {
			return nil, fmt.Errorf("attempting to symlinkat with an invalid file descriptor %d", dirfd)
		}
		return nil, nil
	})
	return err
}

// InsertReadlinkatResult makes given subsequent call to readlinkat return the specified oldname.
func (sys *SyscallRecorder) InsertReadlinkatResult(call, oldname string) {
	if sys.readlinkats == nil {
		sys.readlinkats = make(map[string]string)
	}
	sys.readlinkats[call] = oldname
}

// Readlinkat is a fake implementation of osutil.Readlinkat (syscall.Readlinkat is not exposed)
func (sys *SyscallRecorder) Readlinkat(dirfd int, path string, buf []byte) (int, error) {
	call := fmt.Sprintf("readlinkat %d %q <ptr>", dirfd, path)
	val, err := sys.rcall(call, func(call string) (interface{}, error) {
		if _, ok := sys.fds[dirfd]; !ok {
			return nil, fmt.Errorf("attempting to readlinkat with an invalid file descriptor %d", dirfd)
		}
		if oldname, ok := sys.readlinkats[call]; ok {
			return oldname, nil
		}
		panic(fmt.Sprintf("one of InsertReadlinkatResult() or InsertFault() for %s must be used", call))
	})
	if err == nil {
		n := copy(buf, val.(string))
		return n, nil
	}
	return 0, err
}

// Remove is a fake implementation of os.Remove
func (sys *SyscallRecorder) Remove(name string) error {
	call := fmt.Sprintf("remove %q", name)
	_, err := sys.rcall(call, nil)
	return err
}

// Fchdir is a fake implementation of syscall.Fchdir
func (sys *SyscallRecorder) Fchdir(fd int) error {
	call := fmt.Sprintf("fchdir %d", fd)
	_, err := sys.rcall(call, func(call string) (interface{}, error) {
		if _, ok := sys.fds[fd]; !ok {
			return nil, fmt.Errorf("attempting to fchdir with an invalid file descriptor %d", fd)
		}
		return nil, nil
	})
	return err
}
