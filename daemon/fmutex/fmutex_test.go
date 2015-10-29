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
	"errors"
	"testing"
	"time"

	"gopkg.in/check.v1"

	"github.com/ubuntu-core/snappy/daemon/fmutex"
)

// Hook up check.v1 into the "go test" runner
func Test(t *testing.T) { check.TestingT(t) }

type fmSuite struct {
	newFlock func() fmutex.FLocker
	lck      testFLocker
}

var _ = check.Suite(&fmSuite{})

type testFLocker struct {
	e error
}

func (n testFLocker) Lock() error   { return n.e }
func (n testFLocker) Unlock() error { return n.e }

func (s *fmSuite) SetUpTest(c *check.C) {
	s.newFlock = fmutex.NewFLock
	s.lck = testFLocker{}
	fmutex.NewFLock = func() fmutex.FLocker {
		return &s.lck
	}
}

func (s fmSuite) TearDownTest(c *check.C) {
	fmutex.NewFLock = s.newFlock
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

func (s *fmSuite) TestLockPanicsOnError(c *check.C) {
	s.lck.e = errors.New("bzzt")
	lck := fmutex.New()

	c.Check(lck.Lock, check.PanicMatches, "unable to lock .* bzzt")
}

func (s *fmSuite) TestUnlockPanicsOnError(c *check.C) {
	lck := fmutex.New()
	lck.Lock()

	s.lck.e = errors.New("bzzt")

	c.Check(lck.Unlock, check.PanicMatches, "unable to unlock .* bzzt")
	s.lck.e = nil
	lck.Unlock()
}

func (s *fmSuite) TestActualPrivMutexPanics(c *check.C) {
	fmutex.NewFLock = s.newFlock
	lck := fmutex.New()

	c.Check(lck.Lock, check.PanicMatches, "unable to lock .* privileges required")

}
