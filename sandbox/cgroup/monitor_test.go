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
	"io/ioutil"
	"os"
	"path"
	"path/filepath"
	"time"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/sandbox/cgroup"
	"github.com/snapcore/snapd/testutil"
)

type monitorSuite struct {
	testutil.BaseTest

	tmp string
	ch  chan string

	inotifyWait time.Duration
}

var _ = Suite(&monitorSuite{})

func (s *monitorSuite) SetUpTest(c *C) {
	s.BaseTest.SetUpTest(c)

	s.tmp = c.MkDir()
	dirs.SetRootDir(s.tmp)
	s.AddCleanup(func() { dirs.SetRootDir("/") })

	s.ch = make(chan string)
	s.AddCleanup(func() { close(s.ch) })

	s.calibrateInotifyDelay(c)
}

func (s *monitorSuite) calibrateInotifyDelay(c *C) {
	folder1 := s.makeTestFolder(c, "folder1")
	filelist := []string{folder1}
	err := cgroup.MonitorDelete(filelist, "test1", s.ch)
	c.Assert(err, IsNil)
	var start time.Time
	go func() {
		start = time.Now()
		err = os.Remove(folder1)
		c.Assert(err, IsNil)
	}()
	<-s.ch
	d := time.Now().Sub(start)
	// On a modern machine the dureation "d" for inotify delivery is
	// around 30-100µs so even the very conservative multiplication means
	// the delay is typcially 3ms-10ms.
	s.inotifyWait = 100 * d
	switch {
	case s.inotifyWait > 1*time.Second:
		s.inotifyWait = 1 * time.Second
	case s.inotifyWait < 10*time.Millisecond:
		s.inotifyWait = 10 * time.Millisecond
	}
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

	time.Sleep(s.inotifyWait)

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
	case <-time.After(2 * s.inotifyWait):
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

	time.Sleep(s.inotifyWait)

	folder3 := s.makeTestFolder(c, "folder3")

	time.Sleep(s.inotifyWait)

	err = os.Remove(folder3)
	c.Assert(err, IsNil)

	// Wait for two seconds to ensure that nothing spurious
	// is received from the channel due to creating or
	// removing folder3
	select {
	case event := <-s.ch:
		c.Fatalf("unexpected channel read of event %q", event)
	case <-time.After(2 * s.inotifyWait):
	}
	err = os.Remove(folder1)
	c.Assert(err, IsNil)
	// Only one file has been removed, so wait two seconds
	// two ensure that nothing spurious is received from
	// the channel
	select {
	case event := <-s.ch:
		c.Fatalf("unexpected channel read of event %q", event)
	case <-time.After(2 * s.inotifyWait):
	}
	err = os.Remove(folder2)
	c.Assert(err, IsNil)

	// All files have been deleted, so NOW we must receive
	// something from the channel
	event := <-s.ch
	c.Assert(event, Equals, "test2")
}

func (s *monitorSuite) TestMonitorSnapSnapAlreadyStopped(c *C) {
	// Note that there is no dir created in this test so
	// this checks that the monitoring is correct is there
	// is no dir
	nonExistingFolder := path.Join(s.tmp, "non-exiting-dir")

	filelist := []string{nonExistingFolder}
	err := cgroup.MonitorDelete(filelist, "test3", s.ch)
	c.Assert(err, Equals, nil)

	event := <-s.ch
	c.Assert(event, Equals, "test3")
}

func (s *monitorSuite) TestMonitorSnapTwoProcessesAtTheSameTime(c *C) {
	folder1 := s.makeTestFolder(c, "folder1")
	folder2 := s.makeTestFolder(c, "folder2")

	filelist1 := []string{folder1}
	filelist2 := []string{folder2}

	channel1 := make(chan string)
	defer close(channel1)

	channel2 := make(chan string)
	defer close(channel2)

	err := cgroup.MonitorDelete(filelist1, "test4a", channel1)
	c.Assert(err, Equals, nil)
	err = cgroup.MonitorDelete(filelist2, "test4b", channel2)
	c.Assert(err, Equals, nil)

	time.Sleep(s.inotifyWait)

	folder3 := s.makeTestFolder(c, "folder3")

	time.Sleep(s.inotifyWait)

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
	case <-time.After(2 * s.inotifyWait):
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
	case <-time.After(2 * s.inotifyWait):
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
	case <-time.After(s.inotifyWait):
	}
	c.Assert(receivedEvent, Equals, "test4b")
}

func (s *monitorSuite) TestMonitorSnapEndedNonExisting(c *C) {
	err := cgroup.MonitorSnapEnded("non-existing-snap", s.ch)
	c.Assert(err, IsNil)

	event := <-s.ch
	c.Check(event, Equals, "non-existing-snap")
}

func (s *monitorSuite) TestMonitorSnapEndedIntegration(c *C) {
	restore := cgroup.MockVersion(cgroup.V2, nil)
	s.AddCleanup(restore)

	// make mock cgroups.procs file
	mockProcsFile := filepath.Join(dirs.GlobalRootDir, "/sys/fs/cgroup/user.slice/user-1000.slice/user@1000.service/app.slice/snap.firefox.firefox.fa61f25b-92e1-4316-8acb-2b95af841855.scope/cgroup.procs")
	err := os.MkdirAll(filepath.Dir(mockProcsFile), 0755)
	c.Assert(err, IsNil)
	err = ioutil.WriteFile(mockProcsFile, []byte("57003\n57004"), 0644)
	c.Assert(err, IsNil)

	// wait for firefox to end
	err = cgroup.MonitorSnapEnded("firefox", s.ch)
	c.Assert(err, IsNil)

	select {
	case snapName := <-s.ch:
		c.Fatalf("unexpected stop reported for snap %v", snapName)
	case <-time.After(2 * s.inotifyWait):
	}

	// simulate cgroup getting removed because firefox stopped
	err = os.RemoveAll(filepath.Dir(mockProcsFile))
	c.Assert(err, IsNil)

	// validate the stoppedSnap got delivered
	snapName := <-s.ch
	c.Check(snapName, Equals, "firefox")
}
