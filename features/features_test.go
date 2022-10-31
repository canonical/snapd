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
	"github.com/snapcore/snapd/overlord/configstate/config"
	"github.com/snapcore/snapd/overlord/state"
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
	c.Check(features.RefreshAppAwareness.String(), Equals, "refresh-app-awareness")
	c.Check(features.ClassicPreservesXdgRuntimeDir.String(), Equals, "classic-preserves-xdg-runtime-dir")
	c.Check(features.RobustMountNamespaceUpdates.String(), Equals, "robust-mount-namespace-updates")
	c.Check(features.UserDaemons.String(), Equals, "user-daemons")
	c.Check(features.DbusActivation.String(), Equals, "dbus-activation")
	c.Check(features.HiddenSnapDataHomeDir.String(), Equals, "hidden-snap-folder")
	c.Check(features.MoveSnapHomeDir.String(), Equals, "move-snap-home-dir")
	c.Check(features.CheckDiskSpaceInstall.String(), Equals, "check-disk-space-install")
	c.Check(features.CheckDiskSpaceRefresh.String(), Equals, "check-disk-space-refresh")
	c.Check(features.CheckDiskSpaceRemove.String(), Equals, "check-disk-space-remove")
	c.Check(features.GateAutoRefreshHook.String(), Equals, "gate-auto-refresh-hook")
	c.Check(features.QuotaGroups.String(), Equals, "quota-groups")
	c.Check(func() { _ = features.SnapdFeature(1000).String() }, PanicMatches, "unknown feature flag code 1000")
}

func (*featureSuite) TestKnownFeatures(c *C) {
	// Check that known features have names.
	known := features.KnownFeatures()
	for _, f := range known {
		c.Check(f.String(), Not(Equals), "", Commentf("feature code: %d", int(f)))
	}
	c.Check(known, HasLen, features.NumberOfFeatures())
}

func (*featureSuite) TestIsExported(c *C) {
	c.Check(features.Layouts.IsExported(), Equals, false)
	c.Check(features.Hotplug.IsExported(), Equals, false)
	c.Check(features.SnapdSnap.IsExported(), Equals, false)

	c.Check(features.ParallelInstances.IsExported(), Equals, true)
	c.Check(features.PerUserMountNamespace.IsExported(), Equals, true)
	c.Check(features.RefreshAppAwareness.IsExported(), Equals, true)
	c.Check(features.ClassicPreservesXdgRuntimeDir.IsExported(), Equals, true)
	c.Check(features.UserDaemons.IsExported(), Equals, false)
	c.Check(features.DbusActivation.IsExported(), Equals, false)
	c.Check(features.HiddenSnapDataHomeDir.IsExported(), Equals, true)
	c.Check(features.MoveSnapHomeDir.IsExported(), Equals, true)
	c.Check(features.CheckDiskSpaceInstall.IsExported(), Equals, false)
	c.Check(features.CheckDiskSpaceRefresh.IsExported(), Equals, false)
	c.Check(features.CheckDiskSpaceRemove.IsExported(), Equals, false)
	c.Check(features.GateAutoRefreshHook.IsExported(), Equals, false)
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
	c.Check(features.RefreshAppAwareness.IsEnabledWhenUnset(), Equals, true)
	c.Check(features.ClassicPreservesXdgRuntimeDir.IsEnabledWhenUnset(), Equals, true)
	c.Check(features.RobustMountNamespaceUpdates.IsEnabledWhenUnset(), Equals, true)
	c.Check(features.UserDaemons.IsEnabledWhenUnset(), Equals, false)
	c.Check(features.DbusActivation.IsEnabledWhenUnset(), Equals, true)
	c.Check(features.HiddenSnapDataHomeDir.IsEnabledWhenUnset(), Equals, false)
	c.Check(features.MoveSnapHomeDir.IsEnabledWhenUnset(), Equals, false)
	c.Check(features.CheckDiskSpaceInstall.IsEnabledWhenUnset(), Equals, false)
	c.Check(features.CheckDiskSpaceRefresh.IsEnabledWhenUnset(), Equals, false)
	c.Check(features.CheckDiskSpaceRemove.IsEnabledWhenUnset(), Equals, false)
	c.Check(features.GateAutoRefreshHook.IsEnabledWhenUnset(), Equals, false)
}

func (*featureSuite) TestControlFile(c *C) {
	c.Check(features.PerUserMountNamespace.ControlFile(), Equals, "/var/lib/snapd/features/per-user-mount-namespace")
	c.Check(features.RefreshAppAwareness.ControlFile(), Equals, "/var/lib/snapd/features/refresh-app-awareness")
	c.Check(features.ParallelInstances.ControlFile(), Equals, "/var/lib/snapd/features/parallel-instances")
	c.Check(features.RobustMountNamespaceUpdates.ControlFile(), Equals, "/var/lib/snapd/features/robust-mount-namespace-updates")
	c.Check(features.HiddenSnapDataHomeDir.ControlFile(), Equals, "/var/lib/snapd/features/hidden-snap-folder")
	c.Check(features.MoveSnapHomeDir.ControlFile(), Equals, "/var/lib/snapd/features/move-snap-home-dir")
	// Features that are not exported don't have a control file.
	c.Check(features.Layouts.ControlFile, PanicMatches, `cannot compute the control file of feature "layouts" because that feature is not exported`)
}

func (*featureSuite) TestConfigOptionLayouts(c *C) {
	snapName, configName := features.Layouts.ConfigOption()
	c.Check(snapName, Equals, "core")
	c.Check(configName, Equals, "experimental.layouts")
}

func (*featureSuite) TestConfigOptionRefreshAppAwareness(c *C) {
	snapName, configName := features.RefreshAppAwareness.ConfigOption()
	c.Check(snapName, Equals, "core")
	c.Check(configName, Equals, "experimental.refresh-app-awareness")
}

func (s *featureSuite) TestFlag(c *C) {
	st := state.New(nil)
	st.Lock()
	defer st.Unlock()
	tr := config.NewTransaction(st)

	// Feature flags have a value even if unset.
	flag, err := features.Flag(tr, features.Layouts)
	c.Assert(err, IsNil)
	c.Check(flag, Equals, true)

	// Feature flags can be disabled.
	c.Assert(tr.Set("core", "experimental.layouts", "false"), IsNil)
	flag, err = features.Flag(tr, features.Layouts)
	c.Assert(err, IsNil)
	c.Check(flag, Equals, false)

	// Feature flags can be enabled.
	c.Assert(tr.Set("core", "experimental.layouts", "true"), IsNil)
	flag, err = features.Flag(tr, features.Layouts)
	c.Assert(err, IsNil)
	c.Check(flag, Equals, true)

	// Feature flags must have a well-known value.
	c.Assert(tr.Set("core", "experimental.layouts", "banana"), IsNil)
	_, err = features.Flag(tr, features.Layouts)
	c.Assert(err, ErrorMatches, `layouts can only be set to 'true' or 'false', got "banana"`)
}
