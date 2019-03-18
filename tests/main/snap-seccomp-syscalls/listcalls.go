package main

import (
	"fmt"

	snap_seccomp "github.com/snapcore/snapd/cmd/snap-seccomp/syscalls"
)

func main() {
	for _, syscall := range snap_seccomp.SeccompSyscalls {
		fmt.Println(syscall)
	}
}
