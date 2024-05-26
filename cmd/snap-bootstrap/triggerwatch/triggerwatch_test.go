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

	"github.com/ddkwork/golibrary/mylog"
	"github.com/snapcore/snapd/cmd/snap-bootstrap/triggerwatch"
	"github.com/snapcore/snapd/osutil/udev/netlink"
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
}

func (m *mockTriggerDevice) WaitForTrigger(n chan triggerwatch.KeyEvent) {
	m.withLocked(func() {
		m.waitForTriggerCalls++
		if m.ev != nil {
			ev := *m.ev
			ev.Dev = m
			n <- ev
		}
	})
}

func (m *mockTriggerDevice) String() string { return "mock-device" }
func (m *mockTriggerDevice) Close()         { m.closeCalls++ }

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

const (
	testTriggerTimeout = 5 * time.Millisecond
	testDeviceTimeout  = 2 * time.Millisecond
)

func (s *triggerwatchSuite) TestNoDevsWaitKey(c *C) {
	md := &mockTriggerDevice{ev: &triggerwatch.KeyEvent{}}
	mi := &mockTrigger{d: md}
	restore := triggerwatch.MockInput(mi)
	defer restore()
	mylog.Check(triggerwatch.Wait(testTriggerTimeout, testDeviceTimeout))

	c.Assert(mi.findMatchingCalls, Equals, 1)
	c.Assert(md.waitForTriggerCalls, Equals, 1)
	c.Assert(md.closeCalls, Equals, 1)
}

func (s *triggerwatchSuite) TestNoDevsWaitKeyTimeout(c *C) {
	md := &mockTriggerDevice{}
	mi := &mockTrigger{d: md}
	restore := triggerwatch.MockInput(mi)
	defer restore()
	mylog.Check(triggerwatch.Wait(testTriggerTimeout, testDeviceTimeout))
	c.Assert(err, Equals, triggerwatch.ErrTriggerNotDetected)
	c.Assert(mi.findMatchingCalls, Equals, 1)
	md.withLocked(func() {
		c.Assert(md.waitForTriggerCalls, Equals, 1)
		c.Assert(md.closeCalls, Equals, 1)
	})
}

func (s *triggerwatchSuite) TestNoDevsWaitNoMatching(c *C) {
	mi := &mockTrigger{}
	restore := triggerwatch.MockInput(mi)
	defer restore()
	mylog.Check(triggerwatch.Wait(testTriggerTimeout, testDeviceTimeout))
	c.Assert(err, Equals, triggerwatch.ErrNoMatchingInputDevices)
}

func (s *triggerwatchSuite) TestNoDevsWaitMatchingError(c *C) {
	mi := &mockTrigger{err: fmt.Errorf("failed")}
	restore := triggerwatch.MockInput(mi)
	defer restore()
	mylog.Check(triggerwatch.Wait(testTriggerTimeout, testDeviceTimeout))
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

	md := &mockTriggerDevice{ev: &triggerwatch.KeyEvent{}}
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
	mylog.Check(triggerwatch.Wait(testTriggerTimeout, testDeviceTimeout))

	c.Assert(mi.findMatchingCalls, Equals, 1)

	c.Assert(mi.openCalls, Equals, 1)
	md.withLocked(func() {
		c.Assert(md.waitForTriggerCalls, Equals, 1)
		c.Assert(md.closeCalls, Equals, 1)
	})
}

func (s *triggerwatchSuite) TestUdevEventNoKeyEvent(c *C) {
	nodepath := "/dev/input/event0"
	devpath := "/devices/SOMEBUS/input/input0/event0"

	md := &mockTriggerDevice{}
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
	mylog.Check(triggerwatch.Wait(testTriggerTimeout, testDeviceTimeout))
	c.Assert(err, Equals, triggerwatch.ErrTriggerNotDetected)
	c.Assert(mi.findMatchingCalls, Equals, 1)

	c.Assert(mi.openCalls, Equals, 1)
	md.withLocked(func() {
		c.Assert(md.waitForTriggerCalls, Equals, 1)
		c.Assert(md.closeCalls, Equals, 1)
	})
}
