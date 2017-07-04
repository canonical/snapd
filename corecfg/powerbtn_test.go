// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2017 Canonical Ltd
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

package corecfg_test

import (
	"fmt"
	"io/ioutil"
	"path/filepath"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/corecfg"
)

type powerbtnSuite struct{}

var _ = Suite(&powerbtnSuite{})

func (s *powerbtnSuite) TestConfigurePower(c *C) {
	mockPowerBtnCfg := filepath.Join(c.MkDir(), "00-snap-core.conf")
	restore := corecfg.MockPowerBtnCfg(mockPowerBtnCfg)
	defer restore()

	for _, action := range []string{"ignore", "poweroff", "reboot", "halt", "kexec", "suspend", "hibernate", "hybrid-sleep", "lock"} {
		err := corecfg.SwitchHandlePowerKey(action)
		c.Check(err, IsNil)

		content, err := ioutil.ReadFile(mockPowerBtnCfg)
		c.Assert(err, IsNil)
		c.Check(string(content), Equals, fmt.Sprintf("[Login]\nHandlePowerKey=%s\n", action))
	}
}

func (s *powerbtnSuite) TestConfigurePowerButtonInvalid(c *C) {
	mockPowerBtnCfg := filepath.Join(c.MkDir(), "00-snap-core.conf")
	restore := corecfg.MockPowerBtnCfg(mockPowerBtnCfg)
	defer restore()

	err := corecfg.SwitchHandlePowerKey("invalid-action")
	c.Check(err, ErrorMatches, `invalid action "invalid-action" supplied for system.power-key-action option`)
}
