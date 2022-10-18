// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2022 Canonical Ltd
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

package cgroup_test

import (
	"fmt"
	"io/ioutil"
	"os"
	"time"

	"github.com/google/uuid"
	"github.com/snapcore/snapd/sandbox/cgroup"
	. "gopkg.in/check.v1"
)

type monitorSuite struct{}

var _ = Suite(&monitorSuite{})

func (s *monitorSuite) TestMonitorSnapBasicWork(c *C) {
	tmpfile, err := ioutil.TempFile("", "prefix")
	c.Assert(err, IsNil)

	filelist := []string{tmpfile.Name()}

	channel := make(chan string)

	retval := cgroup.MonitorFullDelete("test1", filelist, channel)
	c.Assert(retval, Equals, nil)

	// Wait for two seconds to ensure that nothing spurious
	// is received from the channel
	for i := 0; i < 2; i++ {
		select {
		case <-channel:
			c.Fail()
		case <-time.After(1 * time.Second):
			continue
		}
	}

	err = os.Remove(tmpfile.Name())
	c.Assert(err, IsNil)
	event := <-channel
	c.Assert(event, Equals, "test1")
}

func (s *monitorSuite) TestMonitorSnapTwoSnapsAtTheSameTime(c *C) {
	tmpfile1, err := ioutil.TempFile("", "prefix")
	c.Assert(err, IsNil)
	tmpfile2, err := ioutil.TempFile("", "prefix")
	c.Assert(err, IsNil)

	filelist := []string{tmpfile1.Name(), tmpfile2.Name()}

	channel := make(chan string)

	retval := cgroup.MonitorFullDelete("test2", filelist, channel)
	c.Assert(retval, Equals, nil)

	// Wait for two seconds to ensure that nothing spurious
	// is received from the channel
	for i := 0; i < 2; i++ {
		select {
		case <-channel:
			c.Fail()
		case <-time.After(1 * time.Second):
			continue
		}
	}
	err = os.Remove(tmpfile1.Name())
	c.Assert(err, IsNil)
	// Only one file has been removed, so wait two seconds
	// two ensure that nothing spurious is received from
	// the channel
	for i := 0; i < 2; i++ {
		select {
		case <-channel:
			c.Fail()
		case <-time.After(1 * time.Second):
			continue
		}
	}
	err = os.Remove(tmpfile2.Name())
	c.Assert(err, IsNil)

	// All files have been deleted, so NOW we must receive
	// something from the channel
	event := <-channel
	c.Assert(event, Equals, "test2")
}

func (s *monitorSuite) TestMonitorSnapSnapAlreadyStopped(c *C) {
	filename := fmt.Sprintf("aFileNameThatDoesntExist%s", uuid.New().String())

	filelist := []string{filename}

	channel := make(chan string)
	retval := cgroup.MonitorFullDelete("test3", filelist, channel)
	c.Assert(retval, Equals, nil)
	event := <-channel
	c.Assert(event, Equals, "test3")
}
