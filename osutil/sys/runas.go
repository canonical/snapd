// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2023 Canonical Ltd
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
	"runtime"
	"syscall"
)

var mockedRestoreUidError error

// MockRunAsUidGidRestoreUidError mocks an error from the calls that
// restore the original euid/egid. Only ever use this in tests.
func MockRunAsUidGidRestoreUidError(err error) (restore func()) {
	oldMockedRestoreUidError := mockedRestoreUidError
	mockedRestoreUidError = err
	return func() {
		mockedRestoreUidError = oldMockedRestoreUidError
	}
}

func composeErr(prefix1 string, err1 error, prefix2 string, err2 error) error {
	switch {
	case err1 != nil && err2 != nil:
		return fmt.Errorf("%v: %v and %v: %v", prefix1, err1, prefix2, err2)
	case err1 != nil:
		return fmt.Errorf("%v: %v", prefix1, err1)
	case err2 != nil:
		return fmt.Errorf("%v: %v", prefix2, err2)
	default:
		return nil
	}
}

// RunAsUidGid starts a goroutine, pins it to the OS thread, sets euid and egid,
// and runs the function; after the function returns, it restores euid and egid.
//
// A caveat is that any go-routine started within RunAsUidGid() will
// run with the original uid/gid and *not* with the passed uid/gid.
//
// Note that on the *kernel* level the user/group ID are per-thread
// attributes. However POSIX require all thread to share the same
// credentials. This is why this code uses RawSyscall() and not the
// syscall.Setreuid() or similar helper.
//
// This function does not add any security (it's not privilidge
// dropping), but it's useful to e.g. manipulate files with the right
// uids/gids.
//
// If restoring the original euid and egid fails this function will let
// the os-thread die otherwise it will be reused by the runtime.
func RunAsUidGid(uid UserID, gid GroupID, f func() error) error {
	ch := make(chan error, 1)
	go func() {
		// from the docs:
		//   until the goroutine exits or calls UnlockOSThread, it will
		//   always execute in this thread, and no other goroutine can.
		// that last bit means it's safe to setuid/setgid in here, as
		// on the kernel level the setuid/setgid syscalls are per-thread
		// and because of the lock the go-runtime will not run any
		// other code in this thread.
		runtime.LockOSThread()

		ruid := Getuid()
		rgid := Getgid()

		// change GID
		if _, _, errno := syscall.RawSyscall(_SYS_SETREGID, FlagID, uintptr(gid), 0); errno != 0 {
			ch <- fmt.Errorf("setregid: %v", errno)
			return
		}

		// change UID
		if _, _, errno := syscall.RawSyscall(_SYS_SETREUID, FlagID, uintptr(uid), 0); errno != 0 {
			ch <- fmt.Errorf("setreuid: %v", errno)
			return
		}

		funcErr := f()

		// only needed for integration testing
		if mockedRestoreUidError != nil {
			ch <- composeErr("cannot run func", funcErr, "mocked restore uid error", mockedRestoreUidError)
			return
		}

		// make sure we restore GID again
		if _, _, errno := syscall.RawSyscall(_SYS_SETREGID, FlagID, uintptr(rgid), 0); errno != 0 {
			ch <- composeErr("cannot run func", funcErr, "cannot restore regid", errno)
			return
		}

		// make sure we restore UID again
		if _, _, errno := syscall.RawSyscall(_SYS_SETREUID, FlagID, uintptr(ruid), 0); errno != 0 {
			ch <- composeErr("cannot run func", funcErr, "cannot restore reuid", errno)
			return
		}

		// *only* unlock if all restoring of the uid/gid
		// worked correctly. The docs say:
		//  If the caller made any permanent changes to the
		//  state of the thread that would affect other
		//  goroutines, it should not call this function and
		//  thus leave the goroutine locked to the OS thread
		//  until the goroutine (and hence the thread)
		//  exits.
		runtime.UnlockOSThread()
		ch <- funcErr
	}()
	return <-ch
}
