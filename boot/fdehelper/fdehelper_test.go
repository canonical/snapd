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

package fdehelper_test

import (
	"path/filepath"
	"testing"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/boot/fdehelper"
	"github.com/snapcore/snapd/testutil"
)

func TestBoot(t *testing.T) { TestingT(t) }

type fdehelperSuite struct {
	testutil.BaseTest
}

var _ = Suite(&fdehelperSuite{})

func (s *fdehelperSuite) TestEnabled(c *C) {
	tmp := c.MkDir()

	c.Check(fdehelper.Enabled(tmp), Equals, false)

	mockFdeRevealKeyPath := filepath.Join(tmp, "/bin/fde-reveal-key")
	cmd := testutil.MockCommand(c, mockFdeRevealKeyPath, "")
	defer cmd.Restore()
	c.Check(fdehelper.Enabled(tmp), Equals, true)
}

func (s *fdehelperSuite) TestRevealHappy(c *C) {
	tmp := c.MkDir()

	mockedSystemdRun := testutil.MockCommand(c, "systemd-run", `printf "%s" revealed-key`)
	defer mockedSystemdRun.Restore()

	revealParams := &fdehelper.RevealParams{
		SealedKey:        []byte("sealed-key"),
		VolumeName:       "volume-name",
		SourceDevicePath: "/dev/device/path",
	}
	revealedKey, err := fdehelper.Reveal(tmp, revealParams)
	c.Assert(err, IsNil)
	c.Check(revealedKey, DeepEquals, []byte("revealed-key"))
	c.Check(mockedSystemdRun.Calls(), DeepEquals, [][]string{
		{"systemd-run",
			"--pipe", "--same-dir", "--wait", "--collect",
			"--service-type=exec",
			"--quiet",
			"--property=RuntimeMaxSec=1m",
			`--property=ExecStopPost=/bin/sh -c 'if [ "$EXIT_STATUS" != 0 ]; then echo "service result: $SERVICE_RESULT" 1>&2; fi'`,
			"--property=SystemCallFilter=~@mount",
			"--property=ProtectSystem=strict",
			tmp + "/bin/fde-reveal-key",
		},
	})
}

func (s *fdehelperSuite) TestRevealSad(c *C) {
	tmp := c.MkDir()

	mockedSystemdRun := testutil.MockCommand(c, "systemd-run", `echo failure ; false`)
	defer mockedSystemdRun.Restore()

	revealParams := &fdehelper.RevealParams{
		SealedKey:        []byte("sealed-key"),
		VolumeName:       "volume-name",
		SourceDevicePath: "/dev/device/path",
	}
	revealedKey, err := fdehelper.Reveal(tmp, revealParams)
	c.Assert(err, ErrorMatches, "cannot reveal fde key: failure")
	c.Check(revealedKey, IsNil)
}
