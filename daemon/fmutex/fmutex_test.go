// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2015 Canonical Ltd
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

package fmutex_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"gopkg.in/check.v1"

	"github.com/ubuntu-core/snappy/daemon/fmutex"
	"github.com/ubuntu-core/snappy/dirs"
)

// Hook up check.v1 into the "go test" runner
func Test(t *testing.T) { check.TestingT(t) }

type fmSuite struct{}

var _ = check.Suite(&fmSuite{})

func (s fmSuite) SetUpTest(c *check.C) {
	dirs.SetRootDir(c.MkDir())
	c.Assert(os.MkdirAll(filepath.Dir(dirs.SnapLockFile), 0755), check.IsNil)
}

func (s fmSuite) TestFileIsLocked(c *check.C) {
	lck := fmutex.New()
	lck.Lock()
	defer lck.Unlock()

	// try to lock the lockfile from a different process
	c.Assert(exec.Command("flock", "--nonblock", dirs.SnapLockFile, "echo").Run(), check.NotNil)
}

func (s fmSuite) TestBasic(c *check.C) {
	ch1 := make(chan bool)
	ch2 := make(chan bool)
	lck := fmutex.New()

	go func() {
		lck.Lock()
		defer lck.Unlock()

		ch1 <- true

		for i := 0; i < 3; i++ {
			ch2 <- true
			time.Sleep(time.Millisecond)
		}
	}()

	go func() {
		<-ch1 // make sure the 1st one goes 1st

		lck.Lock()
		defer lck.Unlock()

		for i := 0; i < 3; i++ {
			ch2 <- false
			time.Sleep(time.Millisecond)
		}
		close(ch2)
	}()

	bs := make([]bool, 0, 6)
	for i := range ch2 {
		bs = append(bs, i)
	}

	c.Check(bs, check.DeepEquals, []bool{true, true, true, false, false, false})
}

func (s *fmSuite) TestPanicsOnError(c *check.C) {
	dirs.SnapLockFile = "/does/not/exist"
	lck := fmutex.New()

	c.Check(lck.Lock, check.PanicMatches, "unable to lock .*")
	c.Check(lck.Unlock, check.PanicMatches, "unable to unlock .*")
}
