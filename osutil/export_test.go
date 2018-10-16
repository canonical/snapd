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
)

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

func MockOsReadlink(f func(string) (string, error)) func() {
	realOsReadlink := osReadlink
	osReadlink = f

	return func() { osReadlink = realOsReadlink }
}

//MockMountInfo mocks content of /proc/self/mountinfo read by IsHomeUsingNFS
func MockMountInfo(text string) (restore func()) {
	old := procSelfMountInfo
	f, err := ioutil.TempFile("", "mountinfo")
	if err != nil {
		panic(fmt.Errorf("cannot open temporary file: %s", err))
	}
	if err := ioutil.WriteFile(f.Name(), []byte(text), 0644); err != nil {
		panic(fmt.Errorf("cannot write mock mountinfo file: %s", err))
	}
	procSelfMountInfo = f.Name()
	return func() {
		os.Remove(procSelfMountInfo)
		procSelfMountInfo = old
	}
}

// MockEtcFstab mocks content of /etc/fstab read by IsHomeUsingNFS
func MockEtcFstab(text string) (restore func()) {
	old := etcFstab
	f, err := ioutil.TempFile("", "fstab")
	if err != nil {
		panic(fmt.Errorf("cannot open temporary file: %s", err))
	}
	if err := ioutil.WriteFile(f.Name(), []byte(text), 0644); err != nil {
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
	old := syscallUname
	syscallUname = f

	return func() {
		syscallUname = old
	}
}
