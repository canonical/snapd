// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2016-2018 Canonical Ltd
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

package udev_test

import (
	"testing"

	. "gopkg.in/check.v1"

	"github.com/ddkwork/golibrary/mylog"
	"github.com/snapcore/snapd/interfaces"
	"github.com/snapcore/snapd/interfaces/udev"
	"github.com/snapcore/snapd/testutil"
)

func Test(t *testing.T) {
	TestingT(t)
}

type uDevSuite struct {
	backend *udev.Backend
}

var _ = Suite(&uDevSuite{})

// Tests for ReloadRules()

func (s *uDevSuite) SetUpTest(c *C) {
	s.backend = &udev.Backend{}
	c.Assert(s.backend.Initialize(nil), IsNil)
}

func (s *uDevSuite) TestReloadUDevRulesRunsUDevAdm(c *C) {
	cmd := testutil.MockCommand(c, "udevadm", "")
	defer cmd.Restore()
	mylog.Check(s.backend.ReloadRules(nil))

	c.Assert(cmd.Calls(), DeepEquals, [][]string{
		{"udevadm", "control", "--reload-rules"},
		{"udevadm", "trigger", "--subsystem-nomatch=input"},
		// FIXME: temporary until spec.TriggerSubsystem() can be
		// called during disconnect
		{"udevadm", "trigger", "--property-match=ID_INPUT_JOYSTICK=1"},
		{"udevadm", "settle", "--timeout=10"},
	})
}

func (s *uDevSuite) TestReloadUDevRulesReportsErrorsFromReloadRules(c *C) {
	cmd := testutil.MockCommand(c, "udevadm", `
if [ "$1" = "control" ]; then
	echo "failure 1"
	exit 1
fi
	`)
	defer cmd.Restore()
	mylog.Check(s.backend.ReloadRules(nil))
	c.Assert(err.Error(), Equals, ""+
		"cannot reload udev rules: exit status 1\n"+
		"udev output:\n"+
		"failure 1\n")
	c.Assert(cmd.Calls(), DeepEquals, [][]string{
		{"udevadm", "control", "--reload-rules"},
	})
}

func (s *uDevSuite) TestReloadUDevRulesIgnoresErrorsFromDefaultTrigger(c *C) {
	cmd := testutil.MockCommand(c, "udevadm", `
if [ "$1" = "trigger" ]; then
	echo "failure 2"
	exit 2
fi
	`)
	defer cmd.Restore()
	mylog.Check(s.backend.ReloadRules(nil))

	c.Assert(cmd.Calls(), DeepEquals, [][]string{
		{"udevadm", "control", "--reload-rules"},
		{"udevadm", "trigger", "--subsystem-nomatch=input"},
		// FIXME: temporary until spec.TriggerSubsystem() can be
		// called during disconnect
		{"udevadm", "trigger", "--property-match=ID_INPUT_JOYSTICK=1"},
		{"udevadm", "settle", "--timeout=10"},
	})
}

func (s *uDevSuite) TestReloadUDevRulesReportsErrorsFromDefaultTriggerSignals(c *C) {
	cmd := testutil.MockCommand(c, "udevadm", `
if [ "$1" = "trigger" ]; then
	echo "failure 2"
	kill -9 $$
fi
	`)
	defer cmd.Restore()
	mylog.Check(s.backend.ReloadRules(nil))
	c.Assert(err, ErrorMatches, `cannot run udev triggers: signal: killed
udev output:
failure 2
`)
	c.Assert(cmd.Calls(), DeepEquals, [][]string{
		{"udevadm", "control", "--reload-rules"},
		{"udevadm", "trigger", "--subsystem-nomatch=input"},
	})
}

func (s *uDevSuite) TestReloadUDevRulesRunsUDevAdmWithSubsystem(c *C) {
	cmd := testutil.MockCommand(c, "udevadm", "")
	defer cmd.Restore()
	mylog.Check(s.backend.ReloadRules([]string{"input"}))

	c.Assert(cmd.Calls(), DeepEquals, [][]string{
		{"udevadm", "control", "--reload-rules"},
		{"udevadm", "trigger", "--subsystem-nomatch=input"},
		{"udevadm", "trigger", "--subsystem-match=input"},
		{"udevadm", "settle", "--timeout=10"},
	})
}

func (s *uDevSuite) TestReloadUDevRulesIgnoresErrorsFromSubsystemTrigger(c *C) {
	cmd := testutil.MockCommand(c, "udevadm", `
if [ "$2" = "--subsystem-match=input" ]; then
	echo "failure 2"
	exit 2
fi
	`)
	defer cmd.Restore()
	mylog.Check(s.backend.ReloadRules([]string{"input"}))

	c.Assert(cmd.Calls(), DeepEquals, [][]string{
		{"udevadm", "control", "--reload-rules"},
		{"udevadm", "trigger", "--subsystem-nomatch=input"},
		{"udevadm", "trigger", "--subsystem-match=input"},
		{"udevadm", "settle", "--timeout=10"},
	})
}

func (s *uDevSuite) TestReloadUDevRulesRunsUDevAdmWithJoystick(c *C) {
	cmd := testutil.MockCommand(c, "udevadm", "")
	defer cmd.Restore()
	mylog.Check(s.backend.ReloadRules([]string{"input/joystick"}))

	c.Assert(cmd.Calls(), DeepEquals, [][]string{
		{"udevadm", "control", "--reload-rules"},
		{"udevadm", "trigger", "--subsystem-nomatch=input"},
		{"udevadm", "trigger", "--property-match=ID_INPUT_JOYSTICK=1"},
		{"udevadm", "settle", "--timeout=10"},
	})
}

func (s *uDevSuite) TestReloadUDevRulesIgnoresErrorsFromJoystickTrigger(c *C) {
	cmd := testutil.MockCommand(c, "udevadm", `
if [ "$2" = "--property-match=ID_INPUT_JOYSTICK=1" ]; then
	echo "failure 2"
	exit 2
fi
	`)
	defer cmd.Restore()
	mylog.Check(s.backend.ReloadRules([]string{"input/joystick"}))

	c.Assert(cmd.Calls(), DeepEquals, [][]string{
		{"udevadm", "control", "--reload-rules"},
		{"udevadm", "trigger", "--subsystem-nomatch=input"},
		{"udevadm", "trigger", "--property-match=ID_INPUT_JOYSTICK=1"},
		{"udevadm", "settle", "--timeout=10"},
	})
}

func (s *uDevSuite) TestReloadUDevRulesRunsUDevAdmWithTwoSubsystems(c *C) {
	cmd := testutil.MockCommand(c, "udevadm", "")
	defer cmd.Restore()
	mylog.Check(s.backend.ReloadRules([]string{"input", "tty"}))

	c.Assert(cmd.Calls(), DeepEquals, [][]string{
		{"udevadm", "control", "--reload-rules"},
		{"udevadm", "trigger", "--subsystem-nomatch=input"},
		{"udevadm", "trigger", "--subsystem-match=input"},
		{"udevadm", "settle", "--timeout=10"},
	})
}

func (s *uDevSuite) TestNoReloadWhenPreseeding(c *C) {
	cmd := testutil.MockCommand(c, "udevadm", "")
	defer cmd.Restore()

	b := udev.Backend{}
	opts := &interfaces.SecurityBackendOptions{
		Preseed: true,
	}
	c.Assert(b.Initialize(opts), IsNil)
	c.Assert(b.ReloadRules(nil), IsNil)
	c.Assert(cmd.Calls(), HasLen, 0)
}
