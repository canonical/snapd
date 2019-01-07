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

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/features"
	"github.com/snapcore/snapd/overlord/configstate/configcore"
	"github.com/snapcore/snapd/testutil"
)

type experimentalSuite struct {
	configcoreSuite
}

var _ = Suite(&experimentalSuite{})

func featureConf(feature features.SnapdFeature) string {
	return "experimental." + feature.String()
}

func (s *experimentalSuite) TestConfigureExperimentalSettingsInvalid(c *C) {
	for _, feature := range features.KnownFeatures() {
		conf := &mockConf{
			state:   s.state,
			changes: map[string]interface{}{featureConf(feature): "foo"},
		}
		err := configcore.Run(conf)
		c.Check(err, ErrorMatches, fmt.Sprintf(`%s can only be set to 'true' or 'false'`, featureConf(feature)))
	}
}

func (s *experimentalSuite) TestConfigureExperimentalSettingsHappy(c *C) {
	for _, feature := range features.KnownFeatures() {
		for _, t := range []string{"true", "false"} {
			conf := &mockConf{
				state: s.state,
				conf:  map[string]interface{}{featureConf(feature): t},
			}
			err := configcore.Run(conf)
			c.Check(err, IsNil)
		}
	}
}

func (s *experimentalSuite) TestExportedFeatures(c *C) {
	conf := &mockConf{
		state: s.state,
		conf:  map[string]interface{}{featureConf(features.PerUserMountNamespace): true},
	}
	err := configcore.Run(conf)
	c.Assert(err, IsNil)
	c.Check(features.PerUserMountNamespace.ControlFile(), testutil.FilePresent)

	delete(conf.changes, "experimental.per-user-mount-namespace")
	err = configcore.Run(conf)
	c.Assert(err, IsNil)
	c.Check(features.PerUserMountNamespace.ControlFile(), testutil.FilePresent)
}
