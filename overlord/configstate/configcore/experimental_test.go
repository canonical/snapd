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

	"github.com/ddkwork/golibrary/mylog"
	"github.com/snapcore/snapd/features"
	"github.com/snapcore/snapd/osutil"
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
		mylog.Check(configcore.FilesystemOnlyRun(classicDev, conf))
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
			mylog.Check(configcore.FilesystemOnlyRun(classicDev, conf))
			c.Check(err, IsNil)
		}
	}
}

func (s *experimentalSuite) TestExportedFeatures(c *C) {
	conf := &mockConf{
		state: s.state,
		conf:  map[string]interface{}{featureConf(features.PerUserMountNamespace): true},
	}
	mylog.Check(configcore.FilesystemOnlyRun(classicDev, conf))

	c.Check(features.PerUserMountNamespace.ControlFile(), testutil.FilePresent)

	delete(conf.changes, "experimental.per-user-mount-namespace")
	mylog.Check(configcore.FilesystemOnlyRun(classicDev, conf))

	c.Check(features.PerUserMountNamespace.ControlFile(), testutil.FilePresent)
}

func (s *experimentalSuite) TestFilesystemOnlyApply(c *C) {
	conf := configcore.PlainCoreConfig(map[string]interface{}{
		"experimental.refresh-app-awareness": "true",
	})
	tmpDir := c.MkDir()
	c.Assert(configcore.FilesystemOnlyApply(classicDev, tmpDir, conf), IsNil)
	c.Check(osutil.FileExists(filepath.Join(tmpDir, "/var/lib/snapd/features/refresh-app-awareness")), Equals, true)
}

func (s *experimentalSuite) TestFilesystemOnlyApplyValidationFails(c *C) {
	conf := configcore.PlainCoreConfig(map[string]interface{}{
		"experimental.refresh-app-awareness": 1,
	})
	tmpDir := c.MkDir()
	c.Assert(configcore.FilesystemOnlyApply(classicDev, tmpDir, conf), ErrorMatches, `experimental.refresh-app-awareness can only be set to 'true' or 'false'`)
}
