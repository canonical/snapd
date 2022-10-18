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

	var filelist []string
	filelist = append(filelist, tmpfile.Name())

	channel := make(chan string)

	retval := cgroup.MonitorFiles("test1", filelist, channel)
	c.Assert(retval, Equals, false)

	for i := 0; i < 2; i++ {
		select {
		case <-channel:
			c.Fail()
		case <-time.After(1 * time.Second):
			continue
		}
	}

	os.Remove(tmpfile.Name())
	event := <-channel
	c.Assert(event, Equals, "test1")
}

func (s *monitorSuite) TestMonitorSnapTwoSnapsAtTheSameTime(c *C) {
	tmpfile1, err := ioutil.TempFile("", "prefix")
	c.Assert(err, IsNil)
	tmpfile2, err := ioutil.TempFile("", "prefix")
	c.Assert(err, IsNil)

	var filelist []string
	filelist = append(filelist, tmpfile1.Name())
	filelist = append(filelist, tmpfile2.Name())

	channel := make(chan string)

	cgroup.MonitorFiles("test2", filelist, channel)

	for i := 0; i < 2; i++ {
		select {
		case <-channel:
			c.Fail()
		case <-time.After(1 * time.Second):
			continue
		}
	}
	os.Remove(tmpfile1.Name())
	for i := 0; i < 2; i++ {
		select {
		case <-channel:
			c.Fail()
		case <-time.After(1 * time.Second):
			continue
		}
	}
	os.Remove(tmpfile2.Name())

	event := <-channel
	c.Assert(event, Equals, "test2")
}

func (s *monitorSuite) TestMonitorSnapSnapAlreadyStopped(c *C) {
	filename := fmt.Sprintf("aFileNameThatDoesntExist%s", uuid.New().String())
	var filelist []string
	filelist = append(filelist, filename)

	channel := make(chan string)
	retval := cgroup.MonitorFiles("test3", filelist, channel)
	c.Assert(retval, Equals, false)
	event := <-channel
	c.Assert(event, Equals, "test3")
}
