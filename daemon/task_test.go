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

package daemon

import (
	"errors"
	"time"

	"github.com/gorilla/mux"
	"gopkg.in/check.v1"
)

type taskSuite struct{}

var _ = check.Suite(&taskSuite{})

func (s *taskSuite) TestTask(c *check.C) {
	router := mux.NewRouter()
	route := router.Handle("/xyzzy/{uuid}", nil)

	ch := make(chan struct{})

	t := RunTask(func() interface{} {
		ch <- struct{}{}
		return 42
	})

	c.Check(t.UUID(), check.Equals, t.id.String())
	c.Check(t.Output(), check.IsNil)
	c.Check(t.State(), check.Equals, TaskRunning)
	c.Check(t.Location(route), check.Equals, "/xyzzy/"+t.id.String())

	<-ch

	// let the other guy have a go, just in case
	time.Sleep(time.Millisecond)

	c.Check(t.State(), check.Equals, TaskSucceeded)
	c.Check(t.Output(), check.Equals, 42)
}

func (s *taskSuite) TestTaskAsync(c *check.C) {
	syncCh := make(chan int)
	t := RunTask(func() interface{} {
		chin := make(chan interface{})
		chout := make(chan interface{})

		go func() {
			// it's goroutines all the way down
			defer close(chout)

			<-syncCh

			chout <- "hi"

			<-syncCh
			<-syncCh

			c.Check(<-chin, check.Equals, "hello")

			<-syncCh
		}()

		return [2]chan interface{}{chin, chout}
	})

	// a bit of a spinlock
	done := false
	for i := 0; i < 20 && !done; i++ {
		time.Sleep(100 * time.Millisecond)
		t.Lock()
		done = t.chin != nil
		t.Unlock()
	}
	c.Assert(done, check.Equals, true)

	// ok, so we have t.chin
	// before the first chout from the inner goroutine, the task's output is nil
	t.Lock()
	c.Check(t.output, check.IsNil)
	t.Unlock()

	// tick tick
	syncCh <- 1
	syncCh <- 2

	// now we have to wait for tick itself. Another spinlock'll do.
	done = false
	for i := 0; i < 10 && !done; i++ {
		time.Sleep(100 * time.Millisecond)
		t.Lock()
		done = t.output != nil
		t.Unlock()
	}

	// after the first chout, it's the output
	t.Lock()
	c.Check(t.output, check.Equals, "hi")
	t.Unlock()

	syncCh <- 3

	t.Send("hello")

	syncCh <- 4
}

func (s *taskSuite) TestFails(c *check.C) {
	router := mux.NewRouter()
	route := router.Handle("/xyzzy/{uuid}", nil)

	ch := make(chan struct{})
	err := errors.New("everything is broken")

	t := RunTask(func() interface{} {
		ch <- struct{}{}
		return err
	})

	c.Check(t.UUID(), check.Equals, t.id.String())
	c.Check(t.Output(), check.IsNil)
	c.Check(t.State(), check.Equals, TaskRunning)
	c.Check(t.Location(route), check.Equals, "/xyzzy/"+t.id.String())

	<-ch

	// let the other guy have a go, just in case
	time.Sleep(time.Millisecond)

	c.Check(t.State(), check.Equals, TaskFailed)
	c.Check(t.Output(), check.DeepEquals, errorResult{
		Obj: err,
		Str: err.Error(),
	})
}

func (s *taskSuite) TestTickError(c *check.C) {
	// test that task.tick(x) makes task.output an appropriate
	// errorResult when x is an error, and that it returns
	// x, and sets tf

	now := time.Now()
	t := NewTask()
	x := errors.New("an error")

	out := t.tick(x)
	c.Check(out, check.NotNil)
	c.Assert(t.output, check.FitsTypeOf, errorResult{})
	c.Check(t.output.(errorResult).Obj, check.Equals, x)
	c.Check(t.tf.After(now), check.Equals, true)
}

func (s *taskSuite) TestTickNoError(c *check.C) {
	// test that task.tick(x) makes task.output x when x is *not*
	// an error, and that it nil, and sets tf

	now := time.Now()
	t := NewTask()
	x := "not an error"

	out := t.tick(x)
	c.Check(out, check.IsNil)
	c.Check(t.output, check.Equals, x)
	c.Check(t.tf.After(now), check.Equals, true)
}
