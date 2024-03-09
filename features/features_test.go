// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2018-2024 Canonical Ltd
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
	"os"
	"path/filepath"
	"testing"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/features"
	"github.com/snapcore/snapd/overlord/configstate/config"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/systemd"
)

func Test(t *testing.T) { TestingT(t) }

type featureSuite struct{}

var _ = Suite(&featureSuite{})

func (*featureSuite) TestName(c *C) {
	var tested int
	check := func(f features.SnapdFeature, name string) {
		c.Check(f.String(), Equals, name)
		tested++
	}

	check(features.Layouts, "layouts")
	check(features.ParallelInstances, "parallel-instances")
	check(features.Hotplug, "hotplug")
	check(features.SnapdSnap, "snapd-snap")
	check(features.PerUserMountNamespace, "per-user-mount-namespace")
	check(features.RefreshAppAwareness, "refresh-app-awareness")
	check(features.ClassicPreservesXdgRuntimeDir, "classic-preserves-xdg-runtime-dir")
	check(features.RobustMountNamespaceUpdates, "robust-mount-namespace-updates")
	check(features.UserDaemons, "user-daemons")
	check(features.DbusActivation, "dbus-activation")
	check(features.HiddenSnapDataHomeDir, "hidden-snap-folder")
	check(features.MoveSnapHomeDir, "move-snap-home-dir")
	check(features.CheckDiskSpaceInstall, "check-disk-space-install")
	check(features.CheckDiskSpaceRefresh, "check-disk-space-refresh")
	check(features.CheckDiskSpaceRemove, "check-disk-space-remove")
	check(features.GateAutoRefreshHook, "gate-auto-refresh-hook")
	check(features.QuotaGroups, "quota-groups")
	check(features.RefreshAppAwarenessUX, "refresh-app-awareness-ux")
	check(features.AspectsConfiguration, "aspects-configuration")
	check(features.AppArmorPrompting, "apparmor-prompting")

	c.Check(tested, Equals, features.NumberOfFeatures())
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
	var tested int
	check := func(f features.SnapdFeature, exported bool) {
		c.Check(f.IsExported(), Equals, exported)
		tested++
	}

	check(features.Layouts, false)
	check(features.Hotplug, false)
	check(features.SnapdSnap, false)

	check(features.ParallelInstances, true)
	check(features.PerUserMountNamespace, true)
	check(features.RefreshAppAwareness, true)
	check(features.ClassicPreservesXdgRuntimeDir, true)
	check(features.RobustMountNamespaceUpdates, true)
	check(features.UserDaemons, false)
	check(features.DbusActivation, false)
	check(features.HiddenSnapDataHomeDir, true)
	check(features.MoveSnapHomeDir, true)
	check(features.CheckDiskSpaceInstall, false)
	check(features.CheckDiskSpaceRefresh, false)
	check(features.CheckDiskSpaceRemove, false)
	check(features.GateAutoRefreshHook, false)
	check(features.QuotaGroups, false)
	check(features.RefreshAppAwarenessUX, true)
	check(features.AspectsConfiguration, true)
	check(features.AppArmorPrompting, false)

	c.Check(tested, Equals, features.NumberOfFeatures())
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
	err = os.WriteFile(filepath.Join(dirs.FeaturesDir, f.String()), nil, 0644)
	c.Assert(err, IsNil)
	c.Check(f.IsEnabled(), Equals, true)

	// Features that are not exported cannot be queried.
	c.Check(features.Layouts.IsEnabled, PanicMatches, `cannot check if feature "layouts" is enabled because that feature is not exported`)
}

func (*featureSuite) TestIsEnabledWhenUnset(c *C) {
	var tested int
	check := func(f features.SnapdFeature, enabledUnset bool) {
		c.Check(f.IsEnabledWhenUnset(), Equals, enabledUnset)
		tested++
	}

	check(features.Layouts, true)
	check(features.ParallelInstances, false)
	check(features.Hotplug, false)
	check(features.SnapdSnap, false)
	check(features.PerUserMountNamespace, false)
	check(features.RefreshAppAwareness, true)
	check(features.ClassicPreservesXdgRuntimeDir, true)
	check(features.RobustMountNamespaceUpdates, true)
	check(features.UserDaemons, false)
	check(features.DbusActivation, true)
	check(features.HiddenSnapDataHomeDir, false)
	check(features.MoveSnapHomeDir, false)
	check(features.CheckDiskSpaceInstall, false)
	check(features.CheckDiskSpaceRefresh, false)
	check(features.CheckDiskSpaceRemove, false)
	check(features.GateAutoRefreshHook, false)
	check(features.QuotaGroups, false)
	check(features.RefreshAppAwarenessUX, false)
	check(features.AspectsConfiguration, false)
	check(features.AppArmorPrompting, false)

	c.Check(tested, Equals, features.NumberOfFeatures())
}

func (*featureSuite) TestControlFile(c *C) {
	c.Check(features.PerUserMountNamespace.ControlFile(), Equals, "/var/lib/snapd/features/per-user-mount-namespace")
	c.Check(features.RefreshAppAwareness.ControlFile(), Equals, "/var/lib/snapd/features/refresh-app-awareness")
	c.Check(features.ParallelInstances.ControlFile(), Equals, "/var/lib/snapd/features/parallel-instances")
	c.Check(features.RobustMountNamespaceUpdates.ControlFile(), Equals, "/var/lib/snapd/features/robust-mount-namespace-updates")
	c.Check(features.HiddenSnapDataHomeDir.ControlFile(), Equals, "/var/lib/snapd/features/hidden-snap-folder")
	c.Check(features.MoveSnapHomeDir.ControlFile(), Equals, "/var/lib/snapd/features/move-snap-home-dir")
	c.Check(features.RefreshAppAwarenessUX.ControlFile(), Equals, "/var/lib/snapd/features/refresh-app-awareness-ux")
	// Features that are not exported don't have a control file.
	c.Check(features.Layouts.ControlFile, PanicMatches, `cannot compute the control file of feature "layouts" because that feature is not exported`)
	c.Check(features.AspectsConfiguration.ControlFile(), Equals, "/var/lib/snapd/features/aspects-configuration")
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

func (*featureSuite) TestConfigOptionRefreshAppAwarenessUX(c *C) {
	snapName, configName := features.RefreshAppAwarenessUX.ConfigOption()
	c.Check(snapName, Equals, "core")
	c.Check(configName, Equals, "experimental.refresh-app-awareness-ux")
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

func (s *featureSuite) TestAll(c *C) {
	st := state.New(nil)
	st.Lock()
	defer st.Unlock()
	tr := config.NewTransaction(st)

	allFeaturesInfo := features.All(tr)

	// Feature flags are included even if value unset
	layoutsInfo, exists := allFeaturesInfo[features.Layouts.String()]
	c.Assert(exists, Equals, true)
	// Feature flags are supported even if no callback defined.
	c.Check(layoutsInfo.Supported, Equals, true)
	// Feature flags have a value even if unset.
	c.Check(layoutsInfo.Enabled, Equals, true)

	// Feature flags with defined supported callbacks work correctly.

	// Callbacks which return false result in Supported: false
	restore := systemd.MockSystemdVersion(229, nil)
	defer restore()
	allFeaturesInfo = features.All(tr)
	quotaGroupsInfo, exists := allFeaturesInfo[features.QuotaGroups.String()]
	c.Assert(exists, Equals, true)
	c.Check(quotaGroupsInfo.Supported, Equals, false)
	c.Check(quotaGroupsInfo.UnsupportedReason, Matches, "systemd version 229 is too old.*")
	c.Check(quotaGroupsInfo.Enabled, Equals, false)

	// Feature flags can be enabled but unsupported.
	c.Assert(tr.Set("core", "experimental.quota-groups", "true"), IsNil)
	allFeaturesInfo = features.All(tr)
	quotaGroupsInfo, exists = allFeaturesInfo[features.QuotaGroups.String()]
	c.Assert(exists, Equals, true)
	c.Check(quotaGroupsInfo.Supported, Equals, false)
	c.Check(quotaGroupsInfo.UnsupportedReason, Matches, "systemd version 229 is too old.*")
	c.Check(quotaGroupsInfo.Enabled, Equals, true)

	// Callbacks which return true result in Supported: true
	restore = systemd.MockSystemdVersion(230, nil)
	defer restore()
	allFeaturesInfo = features.All(tr)
	quotaGroupsInfo, exists = allFeaturesInfo[features.QuotaGroups.String()]
	c.Assert(exists, Equals, true)
	c.Check(quotaGroupsInfo.Supported, Equals, true)
	c.Check(quotaGroupsInfo.UnsupportedReason, Equals, "")
	c.Check(quotaGroupsInfo.Enabled, Equals, true)

	// Feature flags can be disabled but supported.
	c.Assert(tr.Set("core", "experimental.quota-groups", "false"), IsNil)
	allFeaturesInfo = features.All(tr)
	quotaGroupsInfo, exists = allFeaturesInfo[features.QuotaGroups.String()]
	c.Assert(exists, Equals, true)
	c.Check(quotaGroupsInfo.Supported, Equals, true)
	c.Check(quotaGroupsInfo.UnsupportedReason, Equals, "")
	c.Check(quotaGroupsInfo.Enabled, Equals, false)

	// Feature flags with bad values are omitted, even if supported.
	c.Assert(tr.Set("core", "experimental.quota-groups", "banana"), IsNil)
	allFeaturesInfo = features.All(tr)
	quotaGroupsInfo, exists = allFeaturesInfo[features.QuotaGroups.String()]
	c.Assert(exists, Equals, false)
}
