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

package sys

import (
	"fmt"
	"os"
	"runtime"
	"syscall"
	"unsafe"
)

// FlagID can be passed to chown-ish functions to mean "no change",
// and can be returned from getuid-ish functions to mean "not found".
const FlagID = 1<<32 - 1

// UserID is the type of the system's user identifiers (in C, uid_t).
//
// We give it its own explicit type so you don't have to remember that
// it's a uint32 (which lead to the bug this package fixes in the
// first place)
type UserID uint32

// GroupID is the type of the system's group identifiers (in C, gid_t).
type GroupID uint32

// uid_t is an unsigned 32-bit integer in linux right now.
// so syscall.Gete?[ug]id are wrong, and break in 32 bits
// (see https://github.com/golang/go/issues/22739)
func Getuid() UserID {
	return UserID(getid(_SYS_GETUID))
}

func Geteuid() UserID {
	return UserID(getid(_SYS_GETEUID))
}

func Getgid() GroupID {
	return GroupID(getid(_SYS_GETGID))
}

func Getegid() GroupID {
	return GroupID(getid(_SYS_GETEGID))
}

func getid(id uintptr) uint32 {
	// these are documented as not failing, but see golang#22924
	r0, _, errno := syscall.RawSyscall(id, 0, 0, 0)
	if errno != 0 {
		return uint32(-errno)
	}
	return uint32(r0)
}

func Chown(f *os.File, uid UserID, gid GroupID) error {
	return Fchown(int(f.Fd()), uid, gid)
}

func Fchown(fd int, uid UserID, gid GroupID) error {
	_, _, errno := syscall.Syscall(syscall.SYS_FCHOWN, uintptr(fd), uintptr(uid), uintptr(gid))
	if errno == 0 {
		return nil
	}
	return errno
}

func ChownPath(path string, uid UserID, gid GroupID) error {
	AT_FDCWD := -100 // also written as -0x64 in ztypes_linux_*.go (but -100 in sys_linux_*.s, and /usr/include/linux/fcntl.h)
	return FchownAt(uintptr(AT_FDCWD), path, uid, gid, 0)
}

func FchownAt(dirfd uintptr, path string, uid UserID, gid GroupID, flags int) error {
	p0, err := syscall.BytePtrFromString(path)
	if err != nil {
		return err
	}
	_, _, errno := syscall.Syscall6(syscall.SYS_FCHOWNAT, dirfd, uintptr(unsafe.Pointer(p0)), uintptr(uid), uintptr(gid), uintptr(flags), 0)
	if errno == 0 {
		return nil
	}
	return errno
}

// As of Go 1.9, the O_PATH constant does not seem to be declared
// uniformly over all archtiectures.
const O_PATH = 0x200000

func FcntlGetFl(fd int) (int, error) {
	flags, _, errno := syscall.Syscall(syscall.SYS_FCNTL, uintptr(fd), uintptr(syscall.F_GETFL), 0)
	if errno != 0 {
		return 0, errno
	}
	return int(flags), nil
}

// UnrecoverableError is an error that flags that things have Gone Wrong, the
// runtime is in a bad state, and you should really quit. The intention is that
// if you're trying to recover from a panic and find that the value of the panic
// is an UnrecoverableError, you should just exit ASAP.
type UnrecoverableError struct {
	Call string
	Err  error
}

func (e UnrecoverableError) Error() string {
	return fmt.Sprintf("%s: %v", e.Call, e.Err)
}

// RunAsUidGid starts a goroutine, pins it to the OS thread, sets euid and egid,
// and runs the function; after the function returns, it restores euid and egid.
//
// If restoring the original euid and egid fails this function will panic with
// an UnrecoverableError, and you should _not_ try to recover from it: the
// runtime itself is going to be in trouble.
func RunAsUidGid(uid UserID, gid GroupID, f func() error) error {
	ch := make(chan error, 1)
	go func() {
		// from the docs:
		//   until the goroutine exits or calls UnlockOSThread, it will
		//   always execute in this thread, and no other goroutine can.
		// that last bit means it's safe to setuid/setgid in here, as no
		// other code will run.
		runtime.LockOSThread()

		ruid := Getuid()
		rgid := Getgid()

		if _, _, errno := syscall.RawSyscall(_SYS_SETREGID, FlagID, uintptr(gid), 0); errno == 0 {
			if _, _, errno := syscall.RawSyscall(_SYS_SETREUID, FlagID, uintptr(uid), 0); errno == 0 {
				ch <- f()
				// try to restore euid
				if _, _, errno := syscall.RawSyscall(_SYS_SETREUID, FlagID, uintptr(ruid), 0); errno != 0 {
					// ¯\_(ツ)_/¯
					panic(UnrecoverableError{Call: "setreuid", Err: errno})
				}
			} else {
				ch <- fmt.Errorf("setreuid: %v", errno)
			}

			// try to restore egid
			if _, _, errno := syscall.RawSyscall(_SYS_SETREGID, FlagID, uintptr(rgid), 0); errno != 0 {
				// ¯\_(ツ)_/¯
				panic(UnrecoverableError{Call: "setregid", Err: errno})
			}
		} else {
			ch <- fmt.Errorf("setregid: %v", errno)
		}

		runtime.UnlockOSThread()
	}()
	return <-ch
}
