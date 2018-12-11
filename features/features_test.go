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
package features_test

import (
	"io/ioutil"
	"os"
	"path/filepath"
	"testing"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/features"
)

func Test(t *testing.T) { TestingT(t) }

type featureSuite struct{}

var _ = Suite(&featureSuite{})

func (*featureSuite) TestName(c *C) {
	c.Check(features.Layouts.String(), Equals, "layouts")
	c.Check(features.ParallelInstances.String(), Equals, "parallel-instances")
	c.Check(features.Hotplug.String(), Equals, "hotplug")
	c.Check(features.SnapdSnap.String(), Equals, "snapd-snap")
	c.Check(features.PerUserMountNamespace.String(), Equals, "per-user-mount-namespace")
	c.Check(func() { _ = features.SnapdFeature(1000).String() }, PanicMatches, "unknown feature flag code 1000")
}

func (*featureSuite) TestKnownFeatures(c *C) {
	// Check that known features have names.
	for _, f := range features.KnownFeatures() {
		c.Check(f.String(), Not(Equals), "", Commentf("feature code: %d", int(f)))
	}
}

func (*featureSuite) TestIsExported(c *C) {
	c.Check(features.Layouts.IsExported(), Equals, false)
	c.Check(features.ParallelInstances.IsExported(), Equals, false)
	c.Check(features.Hotplug.IsExported(), Equals, false)
	c.Check(features.SnapdSnap.IsExported(), Equals, false)
	c.Check(features.PerUserMountNamespace.IsExported(), Equals, true)
}

func (*featureSuite) TestIsEnabled(c *C) {
	dirs.SetRootDir(c.MkDir())
	defer dirs.SetRootDir("")

	// If the feature file is absent then the feature is disabled.
	f := features.PerUserMountNamespace
	c.Check(f.IsEnabled(), Equals, false)

	// If the feature file is a regular file then the feature is enabled.
	err := os.MkdirAll(dirs.FeaturesDir, 0755)
	c.Assert(err, IsNil)
	err = ioutil.WriteFile(filepath.Join(dirs.FeaturesDir, f.String()), nil, 0644)
	c.Assert(err, IsNil)
	c.Check(f.IsEnabled(), Equals, true)

	// Features that are not exported cannot be queried.
	c.Check(features.Layouts.IsEnabled, PanicMatches, `cannot check if feature "layouts" is enabled because that feature is not exported`)
}

func (*featureSuite) TestIsEnabledWhenUnset(c *C) {
	c.Check(features.Layouts.IsEnabledWhenUnset(), Equals, true)
	c.Check(features.ParallelInstances.IsEnabledWhenUnset(), Equals, false)
	c.Check(features.Hotplug.IsEnabledWhenUnset(), Equals, false)
	c.Check(features.SnapdSnap.IsEnabledWhenUnset(), Equals, false)
	c.Check(features.PerUserMountNamespace.IsEnabledWhenUnset(), Equals, false)
}

func (*featureSuite) TestControlFile(c *C) {
	c.Check(features.PerUserMountNamespace.ControlFile(), Equals, "/var/lib/snapd/features/per-user-mount-namespace")
	// Features that are not exported don't have a control file.
	c.Check(features.Layouts.ControlFile, PanicMatches, `cannot compute the control file of feature "layouts" because that feature is not exported`)
}

func (*featureSuite) TestConfigOption(c *C) {
	snapName, configName := features.Layouts.ConfigOption()
	c.Check(snapName, Equals, "core")
	c.Check(configName, Equals, "experimental.layouts")
}
