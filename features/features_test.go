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
	check(features.PerUserMountNamespace, "per-user-mount-namespace")
	check(features.RefreshAppAwareness, "refresh-app-awareness")
	check(features.ClassicPreservesXdgRuntimeDir, "classic-preserves-xdg-runtime-dir")
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
	check(features.Confdb, "confdb")
	check(features.ConfdbControl, "confdb-control")
	check(features.AppArmorPrompting, "apparmor-prompting")
	check(features.ContentCompatLabel, "content-compatibility-label")

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
	check(features.ParallelInstances, true)
	check(features.PerUserMountNamespace, true)
	check(features.RefreshAppAwareness, true)
	check(features.ClassicPreservesXdgRuntimeDir, true)
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
	check(features.Confdb, true)
	check(features.ConfdbControl, false)
	check(features.AppArmorPrompting, true)
	check(features.ContentCompatLabel, false)

	c.Check(tested, Equals, features.NumberOfFeatures())
}

func (*featureSuite) TestQuotaGroupsSupportedCallback(c *C) {
	callback, exists := features.FeaturesSupportedCallbacks[features.QuotaGroups]
	c.Assert(exists, Equals, true)

	restore1 := systemd.MockSystemdVersion(229, nil)
	defer restore1()
	supported, reason := callback()
	c.Check(supported, Equals, false)
	c.Check(reason, Matches, "systemd version 229 is too old.*")

	restore2 := systemd.MockSystemdVersion(230, nil)
	defer restore2()
	supported, reason = callback()
	c.Check(supported, Equals, true)
	c.Check(reason, Equals, "")
}

func (*featureSuite) TestUserDaemonsSupportedCallback(c *C) {
	callback, exists := features.FeaturesSupportedCallbacks[features.UserDaemons]
	c.Assert(exists, Equals, true)

	restore1 := features.MockReleaseSystemctlSupportsUserUnits(func() bool { return false })
	defer restore1()
	supported, reason := callback()
	c.Check(supported, Equals, false)
	c.Check(reason, Matches, "user session daemons are not supported.*")

	restore2 := features.MockReleaseSystemctlSupportsUserUnits(func() bool { return true })
	defer restore2()
	supported, reason = callback()
	c.Check(supported, Equals, true)
	c.Check(reason, Equals, "")
}

func (*featureSuite) TestIsSupported(c *C) {
	fakeFeature := features.SnapdFeature(len(features.KnownFeatures()))

	// Check that feature without callback always returns true
	is, why := fakeFeature.IsSupported()
	c.Check(is, Equals, true)
	c.Check(why, Equals, "")

	var fakeSupported bool
	var fakeReason string
	restore := features.MockFeaturesSupportedCallbacks(map[features.SnapdFeature]func() (bool, string){
		fakeFeature: func() (bool, string) { return fakeSupported, fakeReason },
	})
	defer restore()

	fakeSupported = true
	fakeReason = ""
	is, why = fakeFeature.IsSupported()
	c.Check(is, Equals, true)
	c.Check(why, Equals, "")

	// Check that a non-empty reason is ignored
	fakeSupported = true
	fakeReason = "foo"
	is, why = fakeFeature.IsSupported()
	c.Check(is, Equals, true)
	c.Check(why, Equals, "")

	fakeSupported = false
	fakeReason = "foo"
	is, why = fakeFeature.IsSupported()
	c.Check(is, Equals, false)
	c.Check(why, Equals, "foo")

	// Check that unsupported value does not require reason
	fakeSupported = false
	fakeReason = ""
	is, why = fakeFeature.IsSupported()
	c.Check(is, Equals, false)
	c.Check(why, Equals, "")
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
	check(features.PerUserMountNamespace, false)
	check(features.RefreshAppAwareness, true)
	check(features.ClassicPreservesXdgRuntimeDir, true)
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
	check(features.Confdb, false)
	check(features.AppArmorPrompting, false)
	check(features.ConfdbControl, false)
	check(features.ContentCompatLabel, false)

	c.Check(tested, Equals, features.NumberOfFeatures())
}

func (*featureSuite) TestControlFile(c *C) {
	c.Check(features.PerUserMountNamespace.ControlFile(), Equals, "/var/lib/snapd/features/per-user-mount-namespace")
	c.Check(features.RefreshAppAwareness.ControlFile(), Equals, "/var/lib/snapd/features/refresh-app-awareness")
	c.Check(features.ParallelInstances.ControlFile(), Equals, "/var/lib/snapd/features/parallel-instances")
	c.Check(features.HiddenSnapDataHomeDir.ControlFile(), Equals, "/var/lib/snapd/features/hidden-snap-folder")
	c.Check(features.MoveSnapHomeDir.ControlFile(), Equals, "/var/lib/snapd/features/move-snap-home-dir")
	c.Check(features.RefreshAppAwarenessUX.ControlFile(), Equals, "/var/lib/snapd/features/refresh-app-awareness-ux")
	c.Check(features.Confdb.ControlFile(), Equals, "/var/lib/snapd/features/confdb")
	c.Check(features.AppArmorPrompting.ControlFile(), Equals, "/var/lib/snapd/features/apparmor-prompting")
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

	fakeFeature := features.SnapdFeature(features.NumberOfFeatures())
	fakeFeatureUnsupported := features.SnapdFeature(features.NumberOfFeatures() + 1)
	fakeFeatureUnsetNoCallback := features.SnapdFeature(features.NumberOfFeatures() + 2)
	fakeFeatureDisabled := features.SnapdFeature(features.NumberOfFeatures() + 3)
	fakeFeatureBadFlag := features.SnapdFeature(features.NumberOfFeatures() + 4)
	fakeFeatureUnsupportedUnset := features.SnapdFeature(features.NumberOfFeatures() + 5)

	restore1 := features.MockKnownFeaturesImpl(func() []features.SnapdFeature {
		return []features.SnapdFeature{fakeFeature, fakeFeatureUnsupported, fakeFeatureUnsetNoCallback, fakeFeatureDisabled, fakeFeatureBadFlag, fakeFeatureUnsupportedUnset}
	})
	defer restore1()

	restore2 := features.MockFeatureNames(map[features.SnapdFeature]string{
		fakeFeature:                 "fake-feature",
		fakeFeatureUnsupported:      "fake-feature-unsupported",
		fakeFeatureUnsetNoCallback:  "fake-feature-disabled",
		fakeFeatureDisabled:         "fake-feature-set-disabled",
		fakeFeatureBadFlag:          "fake-feature-bad-flag",
		fakeFeatureUnsupportedUnset: "fake-feature-unsupported-unset",
	})
	defer restore2()

	unsupportedReason := "foo"
	restore3 := features.MockFeaturesSupportedCallbacks(map[features.SnapdFeature]func() (bool, string){
		fakeFeature:                 func() (bool, string) { return true, unsupportedReason },
		fakeFeatureUnsupported:      func() (bool, string) { return false, unsupportedReason },
		fakeFeatureDisabled:         func() (bool, string) { return true, unsupportedReason },
		fakeFeatureBadFlag:          func() (bool, string) { return true, unsupportedReason },
		fakeFeatureUnsupportedUnset: func() (bool, string) { return false, unsupportedReason },
	})
	defer restore3()

	// Enable the two enabled fake features
	c.Assert(tr.Set("core", "experimental."+fakeFeature.String(), "true"), IsNil)
	c.Assert(tr.Set("core", "experimental."+fakeFeatureUnsupported.String(), "true"), IsNil)
	c.Assert(tr.Set("core", "experimental."+fakeFeatureDisabled.String(), "false"), IsNil)
	c.Assert(tr.Set("core", "experimental."+fakeFeatureBadFlag.String(), "banana"), IsNil)

	allFeaturesInfo := features.All(tr)

	c.Assert(len(allFeaturesInfo), Equals, 5)

	// Feature flags are included even if value unset
	fakeFeatureInfo, exists := allFeaturesInfo[fakeFeatureUnsetNoCallback.String()]
	c.Assert(exists, Equals, true)
	// Feature flags are supported even if no callback defined.
	c.Check(fakeFeatureInfo.Supported, Equals, true)
	// Feature flags have a value even if unset.
	c.Check(fakeFeatureInfo.Enabled, Equals, false)

	// A feature can be both unset and unsupported
	fakeFeatureInfo, exists = allFeaturesInfo[fakeFeatureUnsupportedUnset.String()]
	c.Assert(exists, Equals, true)
	c.Check(fakeFeatureInfo.Supported, Equals, false)
	c.Check(fakeFeatureInfo.Enabled, Equals, false)

	// Feature flags with defined supported callbacks work correctly.

	// Feature flags can be enabled but unsupported.
	fakeFeatureInfo, exists = allFeaturesInfo[fakeFeatureUnsupported.String()]
	c.Assert(exists, Equals, true)
	// Callbacks which return false result in Supported: false
	c.Check(fakeFeatureInfo.Supported, Equals, false)
	c.Check(fakeFeatureInfo.UnsupportedReason, Matches, unsupportedReason)
	c.Check(fakeFeatureInfo.Enabled, Equals, true)

	// Callbacks which return true result in Supported: true
	fakeFeatureInfo, exists = allFeaturesInfo[fakeFeature.String()]
	c.Assert(exists, Equals, true)
	c.Check(fakeFeatureInfo.Supported, Equals, true)
	c.Check(fakeFeatureInfo.UnsupportedReason, Equals, "")
	c.Check(fakeFeatureInfo.Enabled, Equals, true)

	// Feature flags can be disabled but supported.
	fakeFeatureInfo, exists = allFeaturesInfo[fakeFeatureDisabled.String()]
	c.Assert(exists, Equals, true)
	c.Check(fakeFeatureInfo.Supported, Equals, true)
	c.Check(fakeFeatureInfo.UnsupportedReason, Equals, "")
	c.Check(fakeFeatureInfo.Enabled, Equals, false)

	// Feature flags with bad values are omitted, even if supported.
	fakeFeatureInfo, exists = allFeaturesInfo[fakeFeatureBadFlag.String()]
	c.Assert(exists, Equals, false)
}
