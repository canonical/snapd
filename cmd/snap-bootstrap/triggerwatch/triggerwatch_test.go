// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2020 Canonical Ltd
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

package triggerwatch_test

import (
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/cmd/snap-bootstrap/triggerwatch"
	"github.com/snapcore/snapd/osutil/udev/netlink"
	"github.com/snapcore/snapd/testtime"
)

// Hook up check.v1 into the "go test" runner
func Test(t *testing.T) { TestingT(t) }

type triggerwatchSuite struct{}

var _ = Suite(&triggerwatchSuite{})

type mockTriggerDevice struct {
	sync.Mutex

	waitForTriggerCalls int
	closeCalls          int
	ev                  *triggerwatch.KeyEvent
	// closes when the device has been waited for
	waitedC chan struct{}
	// closes when the device has been closed
	closedC chan struct{}
}

func newMockTriggerDevice(ev *triggerwatch.KeyEvent) *mockTriggerDevice {
	return &mockTriggerDevice{
		ev:      ev,
		waitedC: make(chan struct{}),
		closedC: make(chan struct{}),
	}

}
func (m *mockTriggerDevice) WaitForTrigger(n chan triggerwatch.KeyEvent) {
	m.withLocked(func() {
		defer close(m.waitedC)
		m.waitForTriggerCalls++
		if m.ev != nil {
			ev := *m.ev
			ev.Dev = m
			n <- ev
		}
	})
}

func (m *mockTriggerDevice) String() string { return "mock-device" }
func (m *mockTriggerDevice) Close() {
	defer close(m.closedC)
	m.closeCalls++
}

func (m *mockTriggerDevice) withLocked(f func()) {
	m.Lock()
	defer m.Unlock()
	f()
}

type mockTrigger struct {
	f               triggerwatch.TriggerCapabilityFilter
	d               *mockTriggerDevice
	unlistedDevices map[string]*mockTriggerDevice

	err error

	findMatchingCalls int
	openCalls         int
}

func (m *mockTrigger) FindMatchingDevices(f triggerwatch.TriggerCapabilityFilter) ([]triggerwatch.TriggerDevice, error) {
	m.findMatchingCalls++

	m.f = f
	if m.err != nil {
		return nil, m.err
	}
	if m.d != nil {
		return []triggerwatch.TriggerDevice{m.d}, nil
	}
	return nil, nil
}

func (m *mockTrigger) Open(filter triggerwatch.TriggerCapabilityFilter, node string) (triggerwatch.TriggerDevice, error) {
	m.openCalls++
	device, ok := m.unlistedDevices[node]
	if !ok {
		return nil, errors.New("Not found")
	} else {
		return device, nil
	}
}

const testTriggerTimeout = 5 * time.Millisecond
const testDeviceTimeout = 2 * time.Millisecond

func (s *triggerwatchSuite) TestNoDevsWaitKey(c *C) {
	md := newMockTriggerDevice(&triggerwatch.KeyEvent{})
	mi := &mockTrigger{d: md}
	restore := triggerwatch.MockInput(mi)
	defer restore()

	err := triggerwatch.Wait(testTriggerTimeout, testDeviceTimeout)
	c.Assert(err, IsNil)
	c.Assert(mi.findMatchingCalls, Equals, 1)
	<-md.closedC
	c.Assert(md.waitForTriggerCalls, Equals, 1)
	c.Assert(md.closeCalls, Equals, 1)
}

func (s *triggerwatchSuite) TestNoDevsWaitKeyTimeout(c *C) {
	md := newMockTriggerDevice(nil)
	mi := &mockTrigger{d: md}
	restore := triggerwatch.MockInput(mi)
	defer restore()

	err := triggerwatch.Wait(testTriggerTimeout, testDeviceTimeout)
	c.Assert(err, Equals, triggerwatch.ErrTriggerNotDetected)
	c.Assert(mi.findMatchingCalls, Equals, 1)
	<-md.closedC
	md.withLocked(func() {
		c.Assert(md.waitForTriggerCalls, Equals, 1)
		c.Assert(md.closeCalls, Equals, 1)
	})
}

func (s *triggerwatchSuite) TestNoDevsWaitNoMatching(c *C) {
	mi := &mockTrigger{}
	restore := triggerwatch.MockInput(mi)
	defer restore()

	err := triggerwatch.Wait(testTriggerTimeout, testDeviceTimeout)
	c.Assert(err, Equals, triggerwatch.ErrNoMatchingInputDevices)
}

func (s *triggerwatchSuite) TestNoDevsWaitMatchingError(c *C) {
	mi := &mockTrigger{err: fmt.Errorf("failed")}
	restore := triggerwatch.MockInput(mi)
	defer restore()

	err := triggerwatch.Wait(testTriggerTimeout, testDeviceTimeout)
	c.Assert(err, ErrorMatches, "cannot list trigger devices: failed")
}

func (s *triggerwatchSuite) TestChecksInput(c *C) {
	restore := triggerwatch.MockInput(nil)
	defer restore()

	c.Assert(func() { triggerwatch.Wait(testTriggerTimeout, testDeviceTimeout) },
		Panics, "trigger is unset")
}

func (s *triggerwatchSuite) TestUdevEvent(c *C) {
	nodepath := "/dev/input/event0"
	devpath := "/devices/SOMEBUS/input/input0/event0"

	md := newMockTriggerDevice(&triggerwatch.KeyEvent{})
	mi := &mockTrigger{
		unlistedDevices: map[string]*mockTriggerDevice{
			"/dev/input/event0": md,
		},
	}
	restore := triggerwatch.MockInput(mi)
	defer restore()

	events := []netlink.UEvent{
		{
			Action: netlink.ADD,
			KObj:   devpath,
			Env: map[string]string{
				"SUBSYSTEM": "input",
				"DEVNAME":   nodepath,
				"DEVPATH":   devpath,
			},
		},
	}
	restoreUevents := triggerwatch.MockUEvent(events)
	defer restoreUevents()

	err := triggerwatch.Wait(testTriggerTimeout, testDeviceTimeout)
	c.Assert(err, IsNil)
	c.Assert(mi.findMatchingCalls, Equals, 1)

	<-md.closedC
	c.Assert(mi.openCalls, Equals, 1)
	md.withLocked(func() {
		c.Assert(md.waitForTriggerCalls, Equals, 1)
		c.Assert(md.closeCalls, Equals, 1)
	})
}

func (s *triggerwatchSuite) TestUdevEventNoKeyEvent(c *C) {
	nodepath := "/dev/input/event0"
	devpath := "/devices/SOMEBUS/input/input0/event0"

	md := newMockTriggerDevice(nil)
	mi := &mockTrigger{
		unlistedDevices: map[string]*mockTriggerDevice{
			"/dev/input/event0": md,
		},
	}
	restore := triggerwatch.MockInput(mi)
	defer restore()

	events := []netlink.UEvent{
		{
			Action: netlink.ADD,
			KObj:   devpath,
			Env: map[string]string{
				"SUBSYSTEM": "input",
				"DEVNAME":   nodepath,
				"DEVPATH":   devpath,
			},
		},
	}
	restoreUevents := triggerwatch.MockUEvent(events)
	defer restoreUevents()

	err := triggerwatch.Wait(testTriggerTimeout, testDeviceTimeout)
	c.Assert(err, Equals, triggerwatch.ErrTriggerNotDetected)
	c.Assert(mi.findMatchingCalls, Equals, 1)

	<-md.closedC
	c.Assert(mi.openCalls, Equals, 1)
	md.withLocked(func() {
		c.Assert(md.waitForTriggerCalls, Equals, 1)
		c.Assert(md.closeCalls, Equals, 1)
	})
}

func (s *triggerwatchSuite) TestWaitMoreKeyboards(c *C) {
	nodepath := "/dev/input/event0"
	devpath := "/devices/SOMEBUS/input/input0/event0"

	md := newMockTriggerDevice(nil)
	md2 := newMockTriggerDevice(&triggerwatch.KeyEvent{})
	mi := &mockTrigger{
		d: md,
		unlistedDevices: map[string]*mockTriggerDevice{
			nodepath: md2,
		},
	}
	restore := triggerwatch.MockInput(mi)
	defer restore()

	uevents := make(chan netlink.UEvent)
	restore = triggerwatch.MockUEventChannel(uevents)
	defer restore()

	timersReadyC := make(chan struct{})

	var timers []*testtime.TestTimer
	restore = triggerwatch.MockTimeAfter(func(d time.Duration) <-chan time.Time {
		if d < 0 {
			panic("Timer with negative duration")
		}

		if len(timers) > 2 {
			panic("unexpected timer, already mocked 2 timers")
		}

		tm := testtime.NewTimer(d)
		timers = append(timers, tm)
		// we are expecting 2 times, one for trigger timeout, and one for device
		// timeout
		if len(timers) == 2 {
			close(timersReadyC)
		}
		return tm.ExpiredC()
	})
	defer restore()

	advanceTime := func(d time.Duration) {
		for _, timer := range timers {
			timer.Elapse(d)
		}
	}

	waitResult := make(chan error)
	triggerTimeout := 10 * time.Second
	devWaitTimeout := 2 * time.Second
	go func() {
		err := triggerwatch.Wait(triggerTimeout, devWaitTimeout)
		waitResult <- err
	}()

	// timers have been created, code goes into for{ select {} } loop
	<-timersReadyC

	// advance the test timers, must be inside (dev wait, trigger) timeouts
	advanceTime(devWaitTimeout + time.Second)

	// send a mock event
	uevents <- netlink.UEvent{
		Action: netlink.ADD,
		KObj:   devpath,
		Env: map[string]string{
			"SUBSYSTEM": "input",
			"DEVNAME":   nodepath,
			"DEVPATH":   devpath,
		},
	}

	<-md.waitedC
	<-md2.waitedC

	select {
	case err := <-waitResult:
		c.Assert(err, IsNil)
		<-md.closedC
		<-md2.closedC
		c.Check(mi.findMatchingCalls, Equals, 1)
		md.withLocked(func() {
			c.Check(md.waitForTriggerCalls, Equals, 1)
			c.Check(md.closeCalls, Equals, 1)
		})
		md2.withLocked(func() {
			c.Check(md2.waitForTriggerCalls, Equals, 1)
			c.Check(md2.closeCalls, Equals, 1)
		})
	case <-time.After(time.Second):
		c.Errorf("Wait did not finish")
	}
}
