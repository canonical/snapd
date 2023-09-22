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

package main_test

import (
	"errors"
	"os"
	"path/filepath"
	"time"

	. "gopkg.in/check.v1"

	main "github.com/snapcore/snapd/cmd/snap-bootstrap"
	"github.com/snapcore/snapd/cmd/snap-bootstrap/triggerwatch"
	"github.com/snapcore/snapd/testutil"
)

func (s *cmdSuite) TestRecoveryChooserTriggerDefaults(c *C) {
	n := 0
	marker := filepath.Join(c.MkDir(), "marker")
	passedTimeout := time.Duration(0)
	passedDeviceTimeout := time.Duration(0)

	restore := main.MockDefaultMarkerFile(marker)
	defer restore()
	restore = main.MockTriggerwatchWait(func(timeout time.Duration, deviceTimeout time.Duration) error {
		passedTimeout = timeout
		passedDeviceTimeout = deviceTimeout
		n++
		// trigger happened
		return nil
	})
	defer restore()

	rest, err := main.Parser().ParseArgs([]string{"recovery-chooser-trigger"})
	c.Assert(err, IsNil)
	c.Assert(rest, HasLen, 0)
	c.Check(n, Equals, 1)
	c.Check(passedTimeout, Equals, main.DefaultTimeout)
	c.Check(passedDeviceTimeout, Equals, main.DefaultDeviceTimeout)
	c.Check(marker, testutil.FilePresent)
}

func (s *cmdSuite) TestRecoveryChooserTriggerNoTrigger(c *C) {
	n := 0
	marker := filepath.Join(c.MkDir(), "marker")

	restore := main.MockDefaultMarkerFile(marker)
	defer restore()
	restore = main.MockTriggerwatchWait(func(_ time.Duration, _ time.Duration) error {
		n++
		// trigger did not happen
		return triggerwatch.ErrTriggerNotDetected
	})
	defer restore()

	_, err := main.Parser().ParseArgs([]string{"recovery-chooser-trigger"})
	c.Assert(err, IsNil)
	c.Check(n, Equals, 1)
	c.Check(marker, testutil.FileAbsent)
}

func (s *cmdSuite) TestRecoveryChooserTriggerTakesOptions(c *C) {
	marker := filepath.Join(c.MkDir(), "foobar")
	n := 0
	passedTimeout := time.Duration(0)
	passedDeviceTimeout := time.Duration(0)

	restore := main.MockTriggerwatchWait(func(timeout time.Duration, deviceTimeout time.Duration) error {
		passedTimeout = timeout
		passedDeviceTimeout = deviceTimeout
		n++
		// trigger happened
		return nil
	})
	defer restore()

	rest, err := main.Parser().ParseArgs([]string{
		"recovery-chooser-trigger",
		"--device-timeout", "1m",
		"--wait-timeout", "2m",
		"--marker-file", marker,
	})
	c.Assert(err, IsNil)
	c.Assert(rest, HasLen, 0)
	c.Check(n, Equals, 1)
	c.Check(passedTimeout, Equals, 2*time.Minute)
	c.Check(passedDeviceTimeout, Equals, 1*time.Minute)
	c.Check(marker, testutil.FilePresent)
}

func (s *cmdSuite) TestRecoveryChooserTriggerDoesNothingWhenMarkerPresent(c *C) {
	marker := filepath.Join(c.MkDir(), "foobar")
	n := 0
	restore := main.MockTriggerwatchWait(func(_ time.Duration, _ time.Duration) error {
		n++
		return errors.New("unexpected call")
	})
	defer restore()

	err := os.WriteFile(marker, nil, 0644)
	c.Assert(err, IsNil)

	rest, err := main.Parser().ParseArgs([]string{
		"recovery-chooser-trigger",
		"--marker-file", marker,
	})
	c.Assert(err, IsNil)
	c.Assert(rest, HasLen, 0)
	// not called
	c.Check(n, Equals, 0)
}

func (s *cmdSuite) TestRecoveryChooserTriggerBadDurationFallback(c *C) {
	n := 0
	passedTimeout := time.Duration(0)
	restore := main.MockDefaultMarkerFile(filepath.Join(c.MkDir(), "marker"))
	defer restore()

	restore = main.MockTriggerwatchWait(func(timeout time.Duration, _ time.Duration) error {
		passedTimeout = timeout
		n++
		// trigger happened
		return triggerwatch.ErrTriggerNotDetected
	})
	defer restore()

	_, err := main.Parser().ParseArgs([]string{
		"recovery-chooser-trigger",
		"--wait-timeout=foobar",
	})
	c.Assert(err, IsNil)
	c.Check(n, Equals, 1)
	c.Check(passedTimeout, Equals, main.DefaultTimeout)
}

func (s *cmdSuite) TestRecoveryChooserTriggerBadDeviceDurationFallback(c *C) {
	n := 0
	passedTimeout := time.Duration(0)
	restore := main.MockDefaultMarkerFile(filepath.Join(c.MkDir(), "marker"))
	defer restore()

	restore = main.MockTriggerwatchWait(func(_ time.Duration, timeout time.Duration) error {
		passedTimeout = timeout
		n++
		// trigger happened
		return triggerwatch.ErrTriggerNotDetected
	})
	defer restore()

	_, err := main.Parser().ParseArgs([]string{
		"recovery-chooser-trigger",
		"--device-timeout=foobar",
	})
	c.Assert(err, IsNil)
	c.Check(n, Equals, 1)
	c.Check(passedTimeout, Equals, main.DefaultDeviceTimeout)
}

func (s *cmdSuite) TestRecoveryChooserTriggerNoInputDevsNoError(c *C) {
	n := 0
	marker := filepath.Join(c.MkDir(), "marker")

	restore := main.MockDefaultMarkerFile(marker)
	defer restore()
	restore = main.MockTriggerwatchWait(func(_ time.Duration, _ time.Duration) error {
		n++
		// no input devices
		return triggerwatch.ErrNoMatchingInputDevices
	})
	defer restore()

	_, err := main.Parser().ParseArgs([]string{"recovery-chooser-trigger"})
	// does not trigger an error
	c.Assert(err, IsNil)
	c.Check(n, Equals, 1)
	c.Check(marker, testutil.FileAbsent)
}
