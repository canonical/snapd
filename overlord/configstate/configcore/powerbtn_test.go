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

package configcore_test

import (
	"fmt"
	"os"
	"path/filepath"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/overlord/configstate/configcore"
	"github.com/snapcore/snapd/release"
	"github.com/snapcore/snapd/testutil"
)

type powerbtnSuite struct {
	configcoreSuite

	mockPowerBtnCfg string
}

var _ = Suite(&powerbtnSuite{})

func (s *powerbtnSuite) SetUpTest(c *C) {
	s.configcoreSuite.SetUpTest(c)
	dirs.SetRootDir(c.MkDir())
	c.Assert(os.MkdirAll(filepath.Join(dirs.GlobalRootDir, "etc"), 0755), IsNil)

	s.mockPowerBtnCfg = filepath.Join(dirs.GlobalRootDir, "/etc/systemd/logind.conf.d/00-snap-core.conf")
}

func (s *powerbtnSuite) TearDownTest(c *C) {
	dirs.SetRootDir("/")
}

func (s *powerbtnSuite) TestConfigurePowerButtonInvalid(c *C) {
	err := configcore.SwitchHandlePowerKey("invalid-action")
	c.Check(err, ErrorMatches, `invalid action "invalid-action" supplied for system.power-key-action option`)
}

func (s *powerbtnSuite) TestConfigurePowerIntegration(c *C) {
	restore := release.MockOnClassic(false)
	defer restore()

	for _, action := range []string{"ignore", "poweroff", "reboot", "halt", "kexec", "suspend", "hibernate", "hybrid-sleep", "lock"} {

		err := configcore.Run(&mockConf{
			state: s.state,
			conf: map[string]interface{}{
				"system.power-key-action": action,
			},
		})
		c.Assert(err, IsNil)

		// ensure nothing gets enabled/disabled when an unsupported
		// service is set for disable
		c.Check(s.mockPowerBtnCfg, testutil.FileEquals, fmt.Sprintf("[Login]\nHandlePowerKey=%s\n", action))
	}

}
