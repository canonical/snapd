package apparmor

import (
	"syscall"
)

func MockSyscall(syscall func(trap, a1, a2, a3 uintptr) (r1, r2 uintptr, err syscall.Errno)) func() {
	old := doSyscall
	doSyscall = syscall
	return func() { doSyscall = old }
}
