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
	"fmt"
	"io"
	"io/ioutil"
	"os"
	"os/exec"
	"os/user"
	"syscall"
	"time"

	"github.com/snapcore/snapd/osutil/sys"
	"github.com/snapcore/snapd/strutil"
	"github.com/snapcore/snapd/testutil"
)

var (
	StreamsEqualChunked  = streamsEqualChunked
	FilesAreEqualChunked = filesAreEqualChunked
	SudoersFile          = sudoersFile
	DoCopyFile           = doCopyFile
)

type Fileish = fileish

func MockMaxCp(new int64) (restore func()) {
	old := maxcp
	maxcp = new
	return func() {
		maxcp = old
	}
}

func MockCopyFile(new func(fileish, fileish, os.FileInfo) error) (restore func()) {
	old := copyfile
	copyfile = new
	return func() {
		copyfile = old
	}
}

func MockOpenFile(new func(string, int, os.FileMode) (fileish, error)) (restore func()) {
	old := openfile
	openfile = new
	return func() {
		openfile = old
	}
}

func MockSyscallSettimeofday(f func(*syscall.Timeval) error) (restore func()) {
	old := syscallSettimeofday
	syscallSettimeofday = f
	return func() {
		syscallSettimeofday = old
	}
}

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

func MockSyscallKill(f func(int, syscall.Signal) error) func() {
	oldSyscallKill := syscallKill
	syscallKill = f
	return func() {
		syscallKill = oldSyscallKill
	}
}

func MockSyscallStatfs(f func(string, *syscall.Statfs_t) error) func() {
	oldSyscallStatfs := syscallStatfs
	syscallStatfs = f
	return func() {
		syscallStatfs = oldSyscallStatfs
	}
}

func MockSyscallGetpgid(f func(int) (int, error)) func() {
	oldSyscallGetpgid := syscallGetpgid
	syscallGetpgid = f
	return func() {
		syscallGetpgid = oldSyscallGetpgid
	}
}

func MockCmdWaitTimeout(timeout time.Duration) func() {
	oldCmdWaitTimeout := cmdWaitTimeout
	cmdWaitTimeout = timeout
	return func() {
		cmdWaitTimeout = oldCmdWaitTimeout
	}
}

func WaitingReaderGuts(r io.Reader) (io.Reader, *exec.Cmd) {
	wr := r.(*waitingReader)
	return wr.reader, wr.cmd
}

func MockChown(f func(*os.File, sys.UserID, sys.GroupID) error) func() {
	oldChown := chown
	chown = f
	return func() {
		chown = oldChown
	}
}

func MockLookPath(new func(string) (string, error)) (restore func()) {
	old := lookPath
	lookPath = new
	return func() {
		lookPath = old
	}
}

func MockhasAddUserExecutable(new func() bool) (restore func()) {
	old := hasAddUserExecutable
	hasAddUserExecutable = new
	return func() {
		hasAddUserExecutable = old
	}
}

func SetAtomicFileRenamed(aw *AtomicFile, renamed bool) {
	aw.renamed = renamed
}

func SetUnsafeIO(b bool) func() {
	oldSnapdUnsafeIO := snapdUnsafeIO
	snapdUnsafeIO = b
	return func() {
		snapdUnsafeIO = oldSnapdUnsafeIO
	}
}

func GetUnsafeIO() bool {
	// a getter so that tests do not attempt to modify that directly
	return snapdUnsafeIO
}

func MockOsReadlink(f func(string) (string, error)) func() {
	realOsReadlink := osReadlink
	osReadlink = f

	return func() { osReadlink = realOsReadlink }
}

// MockEtcFstab mocks content of /etc/fstab read by IsHomeUsingNFS
func MockEtcFstab(text string) (restore func()) {
	old := etcFstab
	f, err := ioutil.TempFile("", "fstab")
	if err != nil {
		panic(fmt.Errorf("cannot open temporary file: %s", err))
	}
	if err := os.WriteFile(f.Name(), []byte(text), 0644); err != nil {
		panic(fmt.Errorf("cannot write mock fstab file: %s", err))
	}
	etcFstab = f.Name()
	return func() {
		if etcFstab == "/etc/fstab" {
			panic("respectfully refusing to remove /etc/fstab")
		}
		os.Remove(etcFstab)
		etcFstab = old
	}
}

// MockUname mocks syscall.Uname as used by MachineName and KernelVersion
func MockUname(f func(*syscall.Utsname) error) (restore func()) {
	r := testutil.Backup(&syscallUname)
	syscallUname = f
	return r
}

var (
	FindUidNoGetentFallback = findUidNoGetentFallback
	FindGidNoGetentFallback = findGidNoGetentFallback

	FindUidWithGetentFallback = findUidWithGetentFallback
	FindGidWithGetentFallback = findGidWithGetentFallback
)

func MockFindUidNoFallback(mock func(name string) (uint64, error)) (restore func()) {
	old := findUidNoGetentFallback
	findUidNoGetentFallback = mock
	return func() { findUidNoGetentFallback = old }
}

func MockFindGidNoFallback(mock func(name string) (uint64, error)) (restore func()) {
	old := findGidNoGetentFallback
	findGidNoGetentFallback = mock
	return func() { findGidNoGetentFallback = old }
}

const MaxSymlinkTries = maxSymlinkTries

var ParseRawEnvironment = parseRawEnvironment

// ParseRawExpandableEnv returns a new expandable environment parsed from key=value strings.
func ParseRawExpandableEnv(entries []string) (ExpandableEnv, error) {
	om := strutil.NewOrderedMap()
	for _, entry := range entries {
		key, value, err := parseEnvEntry(entry)
		if err != nil {
			return ExpandableEnv{}, err
		}
		if om.Get(key) != "" {
			return ExpandableEnv{}, fmt.Errorf("cannot overwrite earlier value of %q", key)
		}
		om.Set(key, value)
	}
	return ExpandableEnv{OrderedMap: om}, nil
}
