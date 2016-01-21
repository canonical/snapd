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
		Message: err.Error(),
	})
}
