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
	"os"
	"path"
	"time"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/sandbox/cgroup"
	"github.com/snapcore/snapd/testutil"
)

const (
	spuriousMontiorChWait = 2 * time.Second
	spuriousWaits         = 1 * time.Second
)

type monitorSuite struct {
	testutil.BaseTest

	tmp string
	ch  chan string
}

var _ = Suite(&monitorSuite{})

func (s *monitorSuite) SetUpTest(c *C) {
	s.BaseTest.SetUpTest(c)

	s.tmp = c.MkDir()
	s.ch = make(chan string)
	s.AddCleanup(func() { close(s.ch) })
}

func (s *monitorSuite) makeTestFolder(c *C, name string) (fullPath string) {
	fullPath = path.Join(s.tmp, name)
	err := os.Mkdir(fullPath, 0755)
	c.Assert(err, IsNil)
	return fullPath
}

func (s *monitorSuite) TestMonitorSnapBasicWork(c *C) {
	folder1 := s.makeTestFolder(c, "folder1")
	folder2 := s.makeTestFolder(c, "folder2")

	filelist := []string{folder1}
	err := cgroup.MonitorDelete(filelist, "test1", s.ch)
	c.Assert(err, IsNil)

	time.Sleep(spuriousWaits)

	err = os.Remove(folder2)
	c.Assert(err, IsNil)

	// Wait for two seconds to ensure that nothing spurious
	// is received from the channel due to removing folder2
	// Wait for two seconds to ensure that nothing spurious
	// is received from the channel due to creating or
	// removing folder3
	select {
	case event := <-s.ch:
		c.Fatalf("unexpected channel read of event %q", event)
	case <-time.After(spuriousMontiorChWait):
	}

	err = os.Remove(folder1)
	c.Assert(err, IsNil)
	event := <-s.ch
	c.Assert(event, Equals, "test1")
}

func (s *monitorSuite) TestMonitorSnapTwoSnapsAtTheSameTime(c *C) {
	folder1 := s.makeTestFolder(c, "folder1")
	folder2 := s.makeTestFolder(c, "folder2")

	filelist := []string{folder1, folder2}
	err := cgroup.MonitorDelete(filelist, "test2", s.ch)
	c.Assert(err, Equals, nil)

	time.Sleep(spuriousWaits)

	folder3 := s.makeTestFolder(c, "folder3")

	time.Sleep(spuriousWaits)

	err = os.Remove(folder3)
	c.Assert(err, IsNil)

	// Wait for two seconds to ensure that nothing spurious
	// is received from the channel due to creating or
	// removing folder3
	select {
	case event := <-s.ch:
		c.Fatalf("unexpected channel read of event %q", event)
	case <-time.After(spuriousMontiorChWait):
	}
	err = os.Remove(folder1)
	c.Assert(err, IsNil)
	// Only one file has been removed, so wait two seconds
	// two ensure that nothing spurious is received from
	// the channel
	select {
	case event := <-s.ch:
		c.Fatalf("unexpected channel read of event %q", event)
	case <-time.After(spuriousMontiorChWait):
	}
	err = os.Remove(folder2)
	c.Assert(err, IsNil)

	// All files have been deleted, so NOW we must receive
	// something from the channel
	event := <-s.ch
	c.Assert(event, Equals, "test2")
}

func (s *monitorSuite) TestMonitorSnapSnapAlreadyStopped(c *C) {
	tmpcontainer := c.MkDir()

	folder1 := path.Join(tmpcontainer, "folder1")

	filelist := []string{folder1}

	channel := make(chan string)
	defer close(channel)

	err := cgroup.MonitorDelete(filelist, "test3", channel)
	c.Assert(err, Equals, nil)
	event := <-channel
	c.Assert(event, Equals, "test3")
}

func (s *monitorSuite) TestMonitorSnapTwoProcessesAtTheSameTime(c *C) {
	tmpcontainer := c.MkDir()

	folder1 := path.Join(tmpcontainer, "folder1")
	err := os.Mkdir(folder1, 0755)
	c.Assert(err, IsNil)

	folder2 := path.Join(tmpcontainer, "folder2")
	err = os.Mkdir(folder2, 0755)
	c.Assert(err, IsNil)

	filelist1 := []string{folder1}
	filelist2 := []string{folder2}

	channel1 := make(chan string)
	defer close(channel1)

	channel2 := make(chan string)
	defer close(channel2)

	err = cgroup.MonitorDelete(filelist1, "test4a", channel1)
	c.Assert(err, Equals, nil)
	err = cgroup.MonitorDelete(filelist2, "test4b", channel2)
	c.Assert(err, Equals, nil)

	time.Sleep(spuriousWaits)

	folder3 := path.Join(tmpcontainer, "folder3")
	err = os.Mkdir(folder3, 0755)
	c.Assert(err, IsNil)

	time.Sleep(spuriousWaits)

	err = os.Remove(folder3)
	c.Assert(err, IsNil)

	// Wait for two seconds to ensure that nothing spurious
	// is received from the channel due to creating or
	// removing folder3
	select {
	case event := <-channel1:
		c.Fatalf("unexpected channel read of event %q", event)
	case event := <-channel2:
		c.Fatalf("unexpected channel read of event %q", event)
	case <-time.After(spuriousMontiorChWait):
	}
	err = os.Remove(folder1)
	c.Assert(err, IsNil)
	// Only one file has been removed, so wait two seconds
	// two ensure that nothing spurious is received from
	// the channel
	var receivedEvent string
	select {
	case receivedEvent = <-channel1:
	case event := <-channel2:
		c.Fatalf("unexpected channel read of event %q", event)
	case <-time.After(spuriousMontiorChWait):
	}

	c.Assert(receivedEvent, Equals, "test4a")
	err = os.Remove(folder2)
	c.Assert(err, IsNil)

	// All files have been deleted, so NOW we must receive
	// something from the channel
	receivedEvent = ""
	select {
	case receivedEvent = <-channel2:
	case event := <-channel1:
		c.Fatalf("unexpected channel read of event %q", event)
	case <-time.After(spuriousWaits):
	}
	c.Assert(receivedEvent, Equals, "test4b")
}
