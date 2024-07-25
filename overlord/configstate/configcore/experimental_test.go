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

	"github.com/snapcore/snapd/features"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/overlord"
	"github.com/snapcore/snapd/overlord/configstate"
	"github.com/snapcore/snapd/overlord/configstate/config"
	"github.com/snapcore/snapd/overlord/configstate/configcore"
	"github.com/snapcore/snapd/overlord/hookstate"
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
		err := configcore.FilesystemOnlyRun(classicDev, conf)
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
			err := configcore.FilesystemOnlyRun(classicDev, conf)
			c.Check(err, IsNil)
		}
	}
}

func (s *experimentalSuite) TestExportedFeatures(c *C) {
	conf := &mockConf{
		state: s.state,
		conf:  map[string]interface{}{featureConf(features.PerUserMountNamespace): true},
	}
	err := configcore.FilesystemOnlyRun(classicDev, conf)
	c.Assert(err, IsNil)
	c.Check(features.PerUserMountNamespace.ControlFile(), testutil.FilePresent)

	delete(conf.changes, "experimental.per-user-mount-namespace")
	err = configcore.FilesystemOnlyRun(classicDev, conf)
	c.Assert(err, IsNil)
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

func (s *experimentalSuite) TestCleanExperimentalFlags(c *C) {
	o := overlord.Mock()
	s.state = o.State()
	hookMgr, err := hookstate.Manager(s.state, o.TaskRunner())
	c.Assert(err, IsNil)

	s.state.Lock()
	t := config.NewTransaction(s.state)
	// Mock an old feature that got out of experimental but still has the config set
	c.Assert(t.Set("core", "experimental.old-stale-flag", true), IsNil)
	// Example of an existing experimental flag
	c.Assert(t.Set("core", "experimental.parallel-instances", true), IsNil)
	t.Commit()
	s.state.Unlock()

	experimentalFlags := make(map[string]bool, 1)
	c.Assert(t.Get("core", "experimental", &experimentalFlags), IsNil)
	c.Check(experimentalFlags, DeepEquals, map[string]bool{
		"old-stale-flag":     true,
		"parallel-instances": true,
	})

	err = configstate.Init(s.state, hookMgr)
	c.Assert(err, IsNil)
	// Create a new transaction to get latest config from state
	s.state.Lock()
	t = config.NewTransaction(s.state)
	s.state.Unlock()
	// configstate.Init cleans up stale experimental flag
	experimentalFlags = nil
	c.Assert(t.Get("core", "experimental", &experimentalFlags), IsNil)
	c.Check(experimentalFlags, DeepEquals, map[string]bool{
		"parallel-instances": true,
	})
}
