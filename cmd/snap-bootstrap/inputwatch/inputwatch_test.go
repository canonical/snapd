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

package inputwatch_test

import (
	"fmt"
	"testing"
	"time"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/cmd/snap-bootstrap/inputwatch"
)

// Hook up check.v1 into the "go test" runner
func Test(t *testing.T) { TestingT(t) }

type inputwatchSuite struct{}

var _ = Suite(&inputwatchSuite{})

type mockInputDevice struct {
	waitForTriggerCalls int
	ev                  *inputwatch.KeyEvent
}

func (m *mockInputDevice) WaitForTrigger(n chan inputwatch.KeyEvent) {
	m.waitForTriggerCalls++
	if m.ev != nil {
		ev := *m.ev
		ev.Dev = m
		n <- ev
	}
}

func (m *mockInputDevice) String() string { return "mock-device" }

type mockInput struct {
	f   inputwatch.InputCapabilityFilter
	d   *mockInputDevice
	err error

	findMatchingCalls int
}

func (m *mockInput) FindMatchingDevices(f inputwatch.InputCapabilityFilter) ([]inputwatch.InputDevice, error) {
	m.findMatchingCalls++

	m.f = f
	if m.err != nil {
		return nil, m.err
	}
	if m.d != nil {
		return []inputwatch.InputDevice{m.d}, nil
	}
	return nil, nil
}

func (s *inputwatchSuite) TestNoDevsWaitKey(c *C) {
	md := &mockInputDevice{ev: &inputwatch.KeyEvent{}}
	mi := &mockInput{d: md}
	restore := inputwatch.MockInput(mi)
	defer restore()

	err := inputwatch.WaitTriggerKey()
	c.Assert(err, IsNil)
	c.Assert(mi.findMatchingCalls, Equals, 1)
	c.Assert(md.waitForTriggerCalls, Equals, 1)

}

func (s *inputwatchSuite) TestNoDevsWaitKeyTimeout(c *C) {
	md := &mockInputDevice{}
	mi := &mockInput{d: md}
	restore := inputwatch.MockInput(mi)
	defer restore()
	restore = inputwatch.MockTimeout(5 * time.Millisecond)
	defer restore()

	err := inputwatch.WaitTriggerKey()
	c.Assert(err, ErrorMatches, "interrupt key not detected")
	c.Assert(mi.findMatchingCalls, Equals, 1)
	c.Assert(md.waitForTriggerCalls, Equals, 1)
}

func (s *inputwatchSuite) TestNoDevsWaitNoMatching(c *C) {
	mi := &mockInput{}
	restore := inputwatch.MockInput(mi)
	defer restore()

	err := inputwatch.WaitTriggerKey()
	c.Assert(err, ErrorMatches, "cannot find matching devices")
}

func (s *inputwatchSuite) TestNoDevsWaitMatchingError(c *C) {
	mi := &mockInput{err: fmt.Errorf("failed")}
	restore := inputwatch.MockInput(mi)
	defer restore()

	err := inputwatch.WaitTriggerKey()
	c.Assert(err, ErrorMatches, "cannot list input devices: failed")
}

func (s *inputwatchSuite) TestChecksInput(c *C) {
	restore := inputwatch.MockInput(nil)
	defer restore()

	c.Assert(func() { inputwatch.WaitTriggerKey() }, Panics, "input is unset")
}
