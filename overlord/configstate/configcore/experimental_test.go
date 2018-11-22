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
	features []string
}

var _ = Suite(&experimentalSuite{
	features: []string{
		"experimental.layouts",
		"experimental.parallel-instances",
		"experimental.hotplug",
		"experimental.snapd-snap",
	},
})

func (s *experimentalSuite) TestConfigureExperimentalSettingsInvalid(c *C) {
	for _, feature := range s.features {
		conf := &mockConf{
			state:   s.state,
			changes: map[string]interface{}{feature: "foo"},
		}
		err := configcore.Run(conf)
		c.Check(err, ErrorMatches, fmt.Sprintf(`%s can only be set to 'true' or 'false'`, feature))
	}
}

func (s *experimentalSuite) TestConfigureExperimentalSettingsHappy(c *C) {
	for _, feature := range s.features {
		for _, t := range []string{"true", "false"} {
			conf := &mockConf{
				state: s.state,
				conf:  map[string]interface{}{feature: t},
			}
			err := configcore.Run(conf)
			c.Check(err, IsNil)
		}
	}
}

func (s *experimentalSuite) TestExportedFeatures(c *C) {
	conf := &mockConf{
		state:   s.state,
		conf:    map[string]interface{}{"experimental.hotplug": true},
		changes: map[string]interface{}{"experimental.layouts": true},
	}

	err := configcore.Run(conf)
	c.Assert(err, IsNil)

	c.Assert(filepath.Join(dirs.FeaturesDir, "hotplug"), testutil.FilePresent)
	c.Assert(filepath.Join(dirs.FeaturesDir, "layouts"), testutil.FilePresent)

	delete(conf.changes, "experimental.layouts")
	err = configcore.Run(conf)
	c.Assert(err, IsNil)

	c.Assert(filepath.Join(dirs.FeaturesDir, "hotplug"), testutil.FilePresent)
	c.Assert(filepath.Join(dirs.FeaturesDir, "layouts"), testutil.FileAbsent)
}
