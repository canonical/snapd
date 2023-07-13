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
// Note that on the *kernel* level the user/group ID are per-thread
// attributes. However POSIX require all thread to share the same
// credentials. This is why this code uses RawSyscall() and not the
// syscall.Setreuid() or similar helper.
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
		defer runtime.UnlockOSThread()

		ruid := Getuid()
		rgid := Getgid()

		// change GID
		if _, _, errno := syscall.RawSyscall(_SYS_SETREGID, FlagID, uintptr(gid), 0); errno != 0 {
			ch <- fmt.Errorf("setregid: %v", errno)
			return
		}

		// make sure we restore GID again
		defer func() {
			if _, _, errno := syscall.RawSyscall(_SYS_SETREGID, FlagID, uintptr(rgid), 0); errno != 0 {
				panic(UnrecoverableError{Call: "setregid", Err: errno})
			}
		}()

		// change UID
		if _, _, errno := syscall.RawSyscall(_SYS_SETREUID, FlagID, uintptr(uid), 0); errno != 0 {
			ch <- fmt.Errorf("setreuid: %v", errno)
			return
		}

		// make sure we restore UID again
		defer func() {
			if _, _, errno := syscall.RawSyscall(_SYS_SETREUID, FlagID, uintptr(ruid), 0); errno != 0 {
				panic(UnrecoverableError{Call: "setreuid", Err: errno})
			}
		}()

		ch <- f()
	}()
	return <-ch
}
