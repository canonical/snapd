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

	var runAsTid int
	sys.MockRunAsUidGidRestoreUidError(fmt.Errorf("boom"))
	mylog.Check(sys.RunAsUidGid(12345, 12345, func() error {
		runAsTid = syscall.Gettid()
		return nil
	}))
	if err.Error() != "mocked restore uid error: boom" {
		fmt.Printf("unexpected error: %q\n", err)
		os.Exit(1)
	}

	// launch a lot of goroutines so we cover all threads with space to spare
	for i := 0; i < N; i++ {
		wg.Add(1)
		go check(tids, uids, i)
	}
	wg.Wait()

	bad := 0
	for _, tid := range tids {
		if tid == runAsTid {
			bad++
		}
	}
	var badUids int
	for _, uid := range uids {
		if uid != origUid {
			badUids++
		}
	}

	status := "OK"
	if bad != 0 || badUids != 0 {
		status = fmt.Sprintf("%d %d BAD!", bad, badUids)
	}

	fmt.Printf("status: %v\n", status)
}
