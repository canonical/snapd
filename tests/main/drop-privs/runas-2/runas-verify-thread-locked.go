package main

import (
	"fmt"
	"os"
	"runtime"
	"sync"
	"syscall"

	"github.com/ddkwork/golibrary/mylog"
	"github.com/snapcore/snapd/osutil/sys"
)

var (
	wg sync.WaitGroup
	mu sync.Mutex
)

func check(tids []int, uids []sys.UserID, n int) {
	// spin
	for i := 0; i < 1<<30; i++ {
	}

	mu.Lock()
	tids[n] = syscall.Gettid()
	uids[n] = sys.Geteuid()
	mu.Unlock()

	wg.Done()
}

func main() {
	N := 2 * runtime.NumCPU()
	tids := make([]int, N)
	uids := make([]sys.UserID, N)
	origUid := sys.Geteuid()
	mylog.Check(sys.RunAsUidGid(12345, 54321, func() error {
		// running in a locked os thread, get the ID
		lockedTid := syscall.Gettid()

		if os.Geteuid() != 12345 {
			return fmt.Errorf("unexpected euid %v", os.Geteuid())
		}
		if os.Getegid() != 54321 {
			return fmt.Errorf("unexpected egid %v", os.Getegid())
		}

		// launch a lot of goroutines so we cover all threads with space to spare
		for i := 0; i < N; i++ {
			wg.Add(1)
			go check(tids, uids, i)
		}
		wg.Wait()

		// now verify all go-routines ran on a different thread as the one we are on
		var badTids int
		for _, tid := range tids {
			if tid == lockedTid {
				badTids++
			}
		}

		// verify that uid was not inheritted, and that we they were run as the
		// original uid
		var badUids int
		for _, uid := range uids {
			if uid != origUid {
				badUids++
			}
		}

		if badTids > 0 || badUids > 0 {
			return fmt.Errorf("bad tids: %d, bad uids: %d", badTids, badUids)
		}
		return nil
	}))
}
