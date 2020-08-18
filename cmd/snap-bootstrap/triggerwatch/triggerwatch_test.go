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
	"fmt"
	"testing"
	"time"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/cmd/snap-bootstrap/triggerwatch"
)

// Hook up check.v1 into the "go test" runner
func Test(t *testing.T) { TestingT(t) }

type triggerwatchSuite struct{}

var _ = Suite(&triggerwatchSuite{})

type mockTriggerDevice struct {
	waitForTriggerCalls int
	closeCalls          int
	ev                  *triggerwatch.KeyEvent
}

func (m *mockTriggerDevice) WaitForTrigger(n chan triggerwatch.KeyEvent) {
	m.waitForTriggerCalls++
	if m.ev != nil {
		ev := *m.ev
		ev.Dev = m
		n <- ev
	}
}

func (m *mockTriggerDevice) String() string { return "mock-device" }
func (m *mockTriggerDevice) Close()         { m.closeCalls++ }

type mockTrigger struct {
	f   triggerwatch.TriggerCapabilityFilter
	d   *mockTriggerDevice
	err error

	findMatchingCalls int
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

const testTriggerTimeout = 5 * time.Millisecond

func (s *triggerwatchSuite) TestNoDevsWaitKey(c *C) {
	md := &mockTriggerDevice{ev: &triggerwatch.KeyEvent{}}
	mi := &mockTrigger{d: md}
	restore := triggerwatch.MockInput(mi)
	defer restore()

	err := triggerwatch.Wait(testTriggerTimeout)
	c.Assert(err, IsNil)
	c.Assert(mi.findMatchingCalls, Equals, 1)
	c.Assert(md.waitForTriggerCalls, Equals, 1)
	c.Assert(md.closeCalls, Equals, 1)
}

func (s *triggerwatchSuite) TestNoDevsWaitKeyTimeout(c *C) {
	md := &mockTriggerDevice{}
	mi := &mockTrigger{d: md}
	restore := triggerwatch.MockInput(mi)
	defer restore()

	err := triggerwatch.Wait(testTriggerTimeout)
	c.Assert(err, Equals, triggerwatch.ErrTriggerNotDetected)
	c.Assert(mi.findMatchingCalls, Equals, 1)
	c.Assert(md.waitForTriggerCalls, Equals, 1)
	c.Assert(md.closeCalls, Equals, 1)
}

func (s *triggerwatchSuite) TestNoDevsWaitNoMatching(c *C) {
	mi := &mockTrigger{}
	restore := triggerwatch.MockInput(mi)
	defer restore()

	err := triggerwatch.Wait(testTriggerTimeout)
	c.Assert(err, Equals, triggerwatch.ErrNoMatchingInputDevices)
}

func (s *triggerwatchSuite) TestNoDevsWaitMatchingError(c *C) {
	mi := &mockTrigger{err: fmt.Errorf("failed")}
	restore := triggerwatch.MockInput(mi)
	defer restore()

	err := triggerwatch.Wait(testTriggerTimeout)
	c.Assert(err, ErrorMatches, "cannot list trigger devices: failed")
}

func (s *triggerwatchSuite) TestChecksInput(c *C) {
	restore := triggerwatch.MockInput(nil)
	defer restore()

	c.Assert(func() { triggerwatch.Wait(testTriggerTimeout) },
		Panics, "trigger is unset")
}
