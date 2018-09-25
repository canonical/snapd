// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2018 Canonical Ltd
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
	"path/filepath"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/overlord/configstate/configcore"
	"github.com/snapcore/snapd/testutil"
)

type experimentalSuite struct {
	configcoreSuite
}

var _ = Suite(&experimentalSuite{})

func (s *experimentalSuite) TestConfigureExperimentalSettingsInvalid(c *C) {
	for setting, value := range map[string]interface{}{
		"experimental.layouts":            "foo",
		"experimental.parallel-instances": "foo",
		"experimental.hotplug":            "foo",
		"experimental.snapd-snap":         "foo",
	} {
		conf := &mockConf{
			state: s.state,
			changes: map[string]interface{}{
				setting: value,
			},
		}

		err := configcore.Run(conf)
		c.Check(err, ErrorMatches, fmt.Sprintf(`%s can only be set to 'true' or 'false'`, setting))
	}
}

func (s *experimentalSuite) TestConfigureExperimentalSettingsHappy(c *C) {
	for _, setting := range []string{"experimental.layouts", "experimental.parallel-instances", "experimental.hotplug"} {
		for _, t := range []string{"true", "false"} {
			conf := &mockConf{
				state: s.state,
				conf: map[string]interface{}{
					setting: t,
				},
			}

			err := configcore.Run(conf)
			c.Check(err, IsNil)
		}
	}
}

func (s *experimentalSuite) TestConfigureKnownOption(c *C) {
	for setting, value := range map[string]interface{}{
		"experimental.layouts":            true,
		"experimental.parallel-instances": false,
	} {
		conf := &mockConf{
			state: s.state,
			changes: map[string]interface{}{
				setting: value,
			},
		}

		err := configcore.Run(conf)
		c.Check(err, IsNil)
	}
}

func (s *experimentalSuite) TestExportedState(c *C) {
	conf := &mockConf{
		state:   s.state,
		conf:    map[string]interface{}{"experimental.hotplug": true},
		changes: map[string]interface{}{"experimental.layouts": true},
	}

	err := configcore.Run(conf)
	c.Check(err, IsNil)

	c.Assert(filepath.Join(dirs.FeaturesDir, "hotplug"), testutil.FileEquals, "")
	c.Assert(filepath.Join(dirs.FeaturesDir, "layouts"), testutil.FileEquals, "")
}
