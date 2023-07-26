package apparmor

import (
	"golang.org/x/sys/unix"
)

func MockSyscall(syscall func(trap, a1, a2, a3 uintptr) (r1, r2 uintptr, err unix.Errno)) func() {
	old := doSyscall
	doSyscall = syscall
	return func() { doSyscall = old }
}
