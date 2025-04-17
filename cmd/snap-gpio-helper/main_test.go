// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2025 Canonical Ltd
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
	"os"
	"testing"

	. "gopkg.in/check.v1"

	main "github.com/snapcore/snapd/cmd/snap-gpio-helper"
	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/features"
	"github.com/snapcore/snapd/testutil"
)

// Hook up check.v1 into the "go test" runner
func Test(t *testing.T) { TestingT(t) }

type snapGpioHelperSuite struct {
	testutil.BaseTest
}

var _ = Suite(&snapGpioHelperSuite{})

func (s *snapGpioHelperSuite) SetUpTest(c *C) {
	dirs.SetRootDir(c.MkDir())
	s.AddCleanup(func() { dirs.SetRootDir("") })

	// Mock experimental.gpio-chardev-interface
	c.Assert(os.MkdirAll(dirs.FeaturesDir, 0755), IsNil)
	c.Assert(os.WriteFile(features.GPIOChardevInterface.ControlFile(), []byte(nil), 0644), IsNil)
}

func (s *snapGpioHelperSuite) TestGpioChardevExperimentlFlagUnset(c *C) {
	c.Assert(os.RemoveAll(features.GPIOChardevInterface.ControlFile()), IsNil)

	err := main.Run([]string{
		"export-chardev", "label-0", "0,2", "gadget-name", "slot-name",
	})
	c.Check(err, ErrorMatches, `gpio-chardev interface requires the "experimental.gpio-chardev-interface" flag to be set`)

	err = main.Run([]string{
		"unexport-chardev", "label-0", "0,2", "gadget-name", "slot-name",
	})
	c.Check(err, ErrorMatches, `gpio-chardev interface requires the "experimental.gpio-chardev-interface" flag to be set`)
}
