package notify

import (
	"golang.org/x/sys/unix"
)

func MockSyscall(syscall func(trap, a1, a2, a3 uintptr) (r1, r2 uintptr, err unix.Errno)) (restore func()) {
	old := doSyscall
	doSyscall = syscall
	restore = func() { doSyscall = old }
	return restore
}
