package main

import (
	"fmt"
	"runtime"
	"sync"

	"github.com/ddkwork/golibrary/mylog"
	"github.com/snapcore/snapd/osutil/sys"
)

var (
	wg sync.WaitGroup
	mu sync.Mutex
)

func check(uids []sys.UserID, n int) {
	// spin
	for i := 0; i < 1<<30; i++ {
	}

	mu.Lock()
	uids[n] = sys.Geteuid()
	mu.Unlock()

	wg.Done()
}

func main() {
	orig := sys.Geteuid()
	before := fmt.Sprintf("%d/%d", sys.Geteuid(), sys.Getegid())
	var during string
	mylog.Check(sys.RunAsUidGid(12345, 12345, func() error {
		during = fmt.Sprintf("%d/%d", sys.Geteuid(), sys.Getegid())
		return nil
	}))
	after := fmt.Sprintf("%d/%d", sys.Geteuid(), sys.Getegid())

	N := 2 * runtime.NumCPU()
	uids := make([]sys.UserID, N)
	// launch a lot of goroutines so we cover all threads with space to spare
	for i := 0; i < N; i++ {
		wg.Add(1)
		go check(uids, i)
	}
	wg.Wait()

	bad := 0
	for _, uid := range uids {
		if uid != orig {
			bad++
		}
	}
	status := "OK"
	if bad != 0 {
		status = fmt.Sprintf("%d BAD!", bad)
	}

	fmt.Printf("before: %s, during: %s (%v), after: %s; status: %s\n", before, during, err, after, status)
}
