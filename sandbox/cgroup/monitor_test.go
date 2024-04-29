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
	"context"
	"os"
	"path"
	"path/filepath"
	"time"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/sandbox/cgroup"
	"github.com/snapcore/snapd/testutil"
)

type monitorSuite struct {
	testutil.BaseTest

	tempDir  string
	eventsCh chan string

	inotifyWait time.Duration
}

var _ = Suite(&monitorSuite{})

func (s *monitorSuite) SetUpTest(c *C) {
	logger.SimpleSetup(nil)
	s.BaseTest.SetUpTest(c)

	dirs.SetRootDir(c.MkDir())
	s.AddCleanup(func() { dirs.SetRootDir("/") })

	s.tempDir = c.MkDir()

	s.eventsCh = make(chan string)
	s.AddCleanup(func() { close(s.eventsCh) })
}

func makeTestFolder(c *C, root string, name string) (fullPath string) {
	fullPath = path.Join(root, name)
	err := os.Mkdir(fullPath, 0755)
	c.Assert(err, IsNil)
	return fullPath
}

func (s *monitorSuite) TestMonitorSnapBasicWork(c *C) {
	cb := 0
	monitorAdded := make(chan struct{})
	s.AddCleanup(cgroup.MockInotifyWatcher(context.Background(), func(w *cgroup.InotifyWatcher, name string) {
		cb++
		switch cb {
		case 1:
			close(monitorAdded)
			c.Check(name, Equals, "test1")
		case 2, 3:
		default:
			c.Fatalf("unexpected callback: %v %q", cb, name)
		}

	}))
	defer cgroup.MockInitWatcherClose()

	folder1 := makeTestFolder(c, s.tempDir, "folder1")
	// canary
	folder2 := makeTestFolder(c, s.tempDir, "folder2")

	filelist := []string{folder1}
	err := cgroup.MonitorDelete(filelist, "test1", s.eventsCh)
	c.Assert(err, IsNil)

	<-monitorAdded

	// removing an unwatched directory does not trigger an event
	err = os.Remove(folder2)
	c.Assert(err, IsNil)

	err = os.Remove(folder1)
	c.Assert(err, IsNil)
	event := <-s.eventsCh
	c.Assert(event, Equals, "test1")
}

func (s *monitorSuite) TestMonitorSnapTwoSnapsAtTheSameTime(c *C) {
	folder1 := makeTestFolder(c, s.tempDir, "folder1")
	folder2 := makeTestFolder(c, s.tempDir, "folder2")
	filelist := []string{folder1, folder2}
	cb := 0
	monitorAdded := make(chan struct{})
	allDone := make(chan struct{})
	s.AddCleanup(cgroup.MockInotifyWatcher(context.Background(), func(w *cgroup.InotifyWatcher, name string) {
		cb++
		switch cb {
		case 1:
			// add for folder1 and folder2
			close(monitorAdded)
			// paths we track + the parent dir is referenced twice
			c.Assert(w.MonitoredDirs(), DeepEquals, map[string]uint{
				s.tempDir: uint(2),
				folder1:   uint(1),
				folder2:   uint(1),
			})
			fallthrough
		case 2, 3, 4:
			// removal of folder1, folder2, folder3
			c.Check(name, Equals, "test2")
			if cb == 4 {
				close(allDone)
				c.Assert(w.MonitoredDirs(), DeepEquals, map[string]uint{})
			}
		default:
			c.Fatalf("unexpected callback: %v %q", cb, name)
		}

	}))
	defer cgroup.MockInitWatcherClose()

	err := cgroup.MonitorDelete(filelist, "test2", s.eventsCh)
	c.Assert(err, Equals, nil)

	<-monitorAdded

	folder3 := makeTestFolder(c, s.tempDir, "folder3")

	time.Sleep(s.inotifyWait)

	err = os.Remove(folder3)
	c.Assert(err, IsNil)

	err = os.Remove(folder1)
	c.Assert(err, IsNil)

	err = os.Remove(folder2)
	c.Assert(err, IsNil)

	// All files have been deleted, so NOW we must receive
	// something from the channel
	event := <-s.eventsCh
	c.Assert(event, Equals, "test2")

	// wait for all events to be processed
	<-allDone
}

func (s *monitorSuite) TestMonitorOverlap(c *C) {
	// add 2 groups with different notification channels monitoring the same
	// set of paths

	folder1 := makeTestFolder(c, s.tempDir, "folder1")
	folder2 := makeTestFolder(c, s.tempDir, "folder2")
	filelist := []string{folder1, folder2}
	cb := 0
	monitorAdded := make(chan struct{}, 2)
	allDone := make(chan struct{})
	s.AddCleanup(cgroup.MockInotifyWatcher(context.Background(), func(w *cgroup.InotifyWatcher, name string) {
		cb++
		switch cb {
		case 1, 2:
			// add for set1 and set2
			monitorAdded <- struct{}{}
			if cb == 1 {
				// paths we track + the parent dir is referenced twice
				c.Assert(w.MonitoredDirs(), DeepEquals, map[string]uint{
					s.tempDir: uint(2),
					folder1:   uint(1),
					folder2:   uint(1),
				})
			} else {
				// previous ref counts + a new full set
				c.Assert(w.MonitoredDirs(), DeepEquals, map[string]uint{
					s.tempDir: uint(4),
					folder1:   uint(2),
					folder2:   uint(2),
				})
			}
			fallthrough
		case 3, 4, 5, 6, 7, 8:
			// removal of folder1, folder2 and then group, for both groups
			if cb == 8 {
				close(allDone)
				c.Assert(w.MonitoredDirs(), DeepEquals, map[string]uint{})
			}
		default:
			c.Fatalf("unexpected callback: %v %q", cb, name)
		}

	}))
	defer cgroup.MockInitWatcherClose()

	set1Ch := make(chan string, 1)
	set2Ch := make(chan string, 1)
	err := cgroup.MonitorDelete(filelist, "set1", set1Ch)
	c.Assert(err, Equals, nil)

	err = cgroup.MonitorDelete(filelist, "set2", set2Ch)
	c.Assert(err, Equals, nil)

	<-monitorAdded
	<-monitorAdded

	err = os.Remove(folder1)
	c.Assert(err, IsNil)

	err = os.Remove(folder2)
	c.Assert(err, IsNil)

	// both sets are notified
	event := <-set1Ch
	c.Assert(event, Equals, "set1")

	event = <-set2Ch
	c.Assert(event, Equals, "set2")

	// wait for all events to be processed
	<-allDone
}

func (s *monitorSuite) TestMonitorSnapSnapAlreadyStopped(c *C) {
	ctx, cancel := context.WithCancel(context.Background())
	s.AddCleanup(cgroup.MockInotifyWatcher(ctx, nil))
	defer cancel()

	// Note that there is no dir created in this test so
	// this checks that the monitoring is correct when there
	// is no dir
	nonExistingFolder := path.Join(c.MkDir(), "non-exiting-dir")

	filelist := []string{nonExistingFolder}
	err := cgroup.MonitorDelete(filelist, "test3", s.eventsCh)
	c.Assert(err, Equals, nil)

	event := <-s.eventsCh
	c.Assert(event, Equals, "test3")
}

func (s *monitorSuite) TestMonitorSnapTwoProcessesAtTheSameTime(c *C) {
	folder1 := makeTestFolder(c, s.tempDir, "folder1")
	folder2 := makeTestFolder(c, s.tempDir, "folder2")
	folder3 := makeTestFolder(c, s.tempDir, "folder3")

	events := 0
	monitorAdded := make(chan string)
	s.AddCleanup(cgroup.MockInotifyWatcher(context.Background(), func(w *cgroup.InotifyWatcher, name string) {
		events++
		switch events {
		case 1, 2:
			// notify about snap monitoring being added
			monitorAdded <- name
			if events == 2 {
				c.Assert(w.MonitoredDirs(), DeepEquals, map[string]uint{
					s.tempDir: uint(2),
					folder1:   uint(1),
					folder2:   uint(1),
				})
			}
		case 3, 4, 5, 6:
			// remove folder3, folder1, folder2
		default:
			c.Fatalf("unexpected callback: %d %q", events, name)
		}
	}))
	defer cgroup.MockInitWatcherClose()

	filelist1 := []string{folder1}
	filelist2 := []string{folder2}

	channel1 := make(chan string)
	defer close(channel1)

	channel2 := make(chan string)
	defer close(channel2)

	err := cgroup.MonitorDelete(filelist1, "test4a", channel1)
	c.Assert(err, Equals, nil)

	c.Assert(<-monitorAdded, Equals, "test4a")

	err = cgroup.MonitorDelete(filelist2, "test4b", channel2)
	c.Assert(err, Equals, nil)

	c.Assert(<-monitorAdded, Equals, "test4b")

	time.Sleep(s.inotifyWait)

	err = os.Remove(folder3)
	c.Assert(err, IsNil)

	err = os.Remove(folder1)
	c.Assert(err, IsNil)
	// Expect to receive an event about the first snap
	var receivedEvent string
	select {
	case receivedEvent = <-channel1:
	case <-time.After(time.Minute):
	}

	c.Assert(receivedEvent, Equals, "test4a")
	err = os.Remove(folder2)
	c.Assert(err, IsNil)

	// Expect to receive an event about the second snap
	receivedEvent = ""
	select {
	case receivedEvent = <-channel2:
	case <-time.After(time.Minute):
	}
	c.Assert(receivedEvent, Equals, "test4b")
}

func (s *monitorSuite) TestMonitorSnapEndedNonExisting(c *C) {
	ctx, cancel := context.WithCancel(context.Background())
	s.AddCleanup(cgroup.MockInotifyWatcher(ctx, nil))
	defer cancel()

	err := cgroup.MonitorSnapEnded("non-existing-snap", s.eventsCh)
	c.Assert(err, IsNil)

	event := <-s.eventsCh
	c.Check(event, Equals, "non-existing-snap")
}

func (s *monitorSuite) TestMonitorSnapEndedIntegration(c *C) {
	ctx, cancel := context.WithCancel(context.Background())
	s.AddCleanup(cgroup.MockInotifyWatcher(ctx, nil))
	defer cancel()

	restore := cgroup.MockVersion(cgroup.V2, nil)
	s.AddCleanup(restore)

	// make mock cgroups.procs file
	mockProcsFile := filepath.Join(dirs.GlobalRootDir, "/sys/fs/cgroup/user.slice/user-1000.slice/user@1000.service/app.slice/snap.firefox.firefox-fa61f25b-92e1-4316-8acb-2b95af841855.scope/cgroup.procs")
	err := os.MkdirAll(filepath.Dir(mockProcsFile), 0755)
	c.Assert(err, IsNil)
	err = os.WriteFile(mockProcsFile, []byte("57003\n57004"), 0644)
	c.Assert(err, IsNil)

	// wait for firefox to end
	err = cgroup.MonitorSnapEnded("firefox", s.eventsCh)
	c.Assert(err, IsNil)

	// TODO observe

	// simulate cgroup getting removed because firefox stopped
	err = os.RemoveAll(filepath.Dir(mockProcsFile))
	c.Assert(err, IsNil)

	// validate the stoppedSnap got delivered
	snapName := <-s.eventsCh
	c.Check(snapName, Equals, "firefox")
}

func (s *monitorSuite) TestMonitorClose(c *C) {
	w := cgroup.NewInotifyWatcher(context.Background())
	f := makeTestFolder(c, s.tempDir, "foo")
	ch := make(chan string)

	err := w.MonitorDelete([]string{f}, "test", ch)
	c.Assert(err, IsNil)

	w.Close()

	select {
	case <-ch:
		c.Fatalf("unexpected event")
	default:
	}
}

func (s *monitorSuite) TestMonitorError(c *C) {
	w := cgroup.NewInotifyWatcher(context.Background())
	ch := make(chan string)

	// XXX note the error isn't propagated back to the caller
	err := w.MonitorDelete([]string{filepath.Join(s.tempDir, "foo/bar/baz")}, "test", ch)
	c.Assert(err, IsNil)

	w.Close()
}
