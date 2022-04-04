// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2016-2020 Canonical Ltd
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

package apparmor_test

import (
	"errors"
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/interfaces"
	"github.com/snapcore/snapd/interfaces/apparmor"
	"github.com/snapcore/snapd/interfaces/ifacetest"
	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/release"
	apparmor_sandbox "github.com/snapcore/snapd/sandbox/apparmor"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/snap/snaptest"
	"github.com/snapcore/snapd/testutil"
	"github.com/snapcore/snapd/timings"
)

type loadProfilesParams struct {
	fnames   []string
	cacheDir string
	flags    apparmor_sandbox.AaParserFlags
}

type removeCachedProfilesParams struct {
	fnames   []string
	cacheDir string
}

type backendSuite struct {
	ifacetest.BackendSuite

	perf *timings.Timings
	meas *timings.Span

	loadProfilesCalls          []loadProfilesParams
	loadProfilesReturn         error
	removeCachedProfilesCalls  []removeCachedProfilesParams
	removeCachedProfilesReturn error
}

var _ = Suite(&backendSuite{})

var testedConfinementOpts = []interfaces.ConfinementOptions{
	{},
	{DevMode: true},
	{JailMode: true},
	{Classic: true},
}

func (s *backendSuite) SetUpTest(c *C) {
	s.Backend = &apparmor.Backend{}
	s.BackendSuite.SetUpTest(c)
	c.Assert(s.Repo.AddBackend(s.Backend), IsNil)

	s.perf = timings.New(nil)
	s.meas = s.perf.StartSpan("", "")

	err := os.MkdirAll(apparmor_sandbox.CacheDir, 0700)
	c.Assert(err, IsNil)

	restore := release.MockReleaseInfo(&release.OS{ID: "ubuntu"})
	s.AddCleanup(restore)

	restore = apparmor_sandbox.MockFeatures(nil, nil, nil, nil)
	s.AddCleanup(restore)

	s.loadProfilesCalls = nil
	s.loadProfilesReturn = nil
	s.removeCachedProfilesCalls = nil
	s.removeCachedProfilesReturn = nil
	restore = apparmor.MockLoadProfiles(func(fnames []string, cacheDir string, flags apparmor_sandbox.AaParserFlags) error {
		// To simplify testing, ignore invocations with no profiles (as a
		// matter of fact, the real implementation is doing the same)
		if len(fnames) == 0 {
			return nil
		}
		s.loadProfilesCalls = append(s.loadProfilesCalls, loadProfilesParams{fnames, cacheDir, flags})
		return s.loadProfilesReturn
	})
	s.AddCleanup(restore)
	restore = apparmor.MockRemoveCachedProfiles(func(fnames []string, cacheDir string) error {
		s.removeCachedProfilesCalls = append(s.removeCachedProfilesCalls, removeCachedProfilesParams{fnames, cacheDir})
		return s.removeCachedProfilesReturn
	})
	s.AddCleanup(restore)

	err = s.Backend.Initialize(ifacetest.DefaultInitializeOpts)
	c.Assert(err, IsNil)
}

func (s *backendSuite) TearDownTest(c *C) {
	s.BackendSuite.TearDownTest(c)
}

// Tests for Setup() and Remove()

func (s *backendSuite) TestName(c *C) {
	c.Check(s.Backend.Name(), Equals, interfaces.SecurityAppArmor)
}

type expSnapConfineTransitionRules struct {
	usrBinSnapRules   bool
	usrLibSnapdTarget string
	coreSnapTarget    string
	snapdSnapTarget   string
}

func checkProfileExtraRules(c *C, profile string, exp expSnapConfineTransitionRules) {
	if exp.usrBinSnapRules {
		c.Assert(profile, testutil.FileContains, "  /usr/bin/snap ixr,")
		c.Assert(profile, testutil.FileContains, " /snap/{snapd,core}/*/usr/bin/snap ixr,")
	} else {
		c.Assert(profile, Not(testutil.FileContains), "/usr/bin/snap")
	}

	if exp.usrLibSnapdTarget != "" {
		rule := fmt.Sprintf("/usr/lib/snapd/snap-confine Pxr -> %s,", exp.usrLibSnapdTarget)
		c.Assert(profile, testutil.FileContains, rule)
	} else {
		c.Assert(profile, Not(testutil.FileMatches), "/usr/lib/snapd/snap-confine")
	}

	if exp.coreSnapTarget != "" {
		rule := fmt.Sprintf("/snap/core/*/usr/lib/snapd/snap-confine Pxr -> %s,", exp.coreSnapTarget)
		c.Assert(profile, testutil.FileContains, rule)
	} else {
		c.Assert(profile, Not(testutil.FileMatches), `/snap/core/\*/usr/lib/snapd/snap-confine`)
	}

	if exp.snapdSnapTarget != "" {
		rule := fmt.Sprintf("/snap/snapd/*/usr/lib/snapd/snap-confine Pxr -> %s,", exp.snapdSnapTarget)
		c.Assert(profile, testutil.FileContains, rule)
	} else {
		c.Assert(profile, Not(testutil.FileMatches), `/snap/snapd/\*/usr/lib/snapd/snap-confine`)
	}
}

func (s *backendSuite) TestInstallingDevmodeSnapCoreSnapOnlyExtraRules(c *C) {
	// re-initialize with new options
	backendOpts := &interfaces.SecurityBackendOptions{
		CoreSnapInfo: ifacetest.DefaultInitializeOpts.CoreSnapInfo,
	}
	err := s.Backend.Initialize(backendOpts)
	c.Assert(err, IsNil)

	devMode := interfaces.ConfinementOptions{DevMode: true}
	s.InstallSnap(c, devMode, "", ifacetest.SambaYamlV1, 1)

	profile := filepath.Join(dirs.SnapAppArmorDir, "snap.samba.smbd")

	checkProfileExtraRules(c, profile, expSnapConfineTransitionRules{
		usrBinSnapRules:   true,
		usrLibSnapdTarget: "/usr/lib/snapd/snap-confine",
		coreSnapTarget:    "/snap/core/123/usr/lib/snapd/snap-confine",
	})
}

func (s *backendSuite) TestInstallingDevmodeSnapSnapdSnapOnlyExtraRules(c *C) {
	// re-initialize with new options
	backendOpts := &interfaces.SecurityBackendOptions{
		SnapdSnapInfo: ifacetest.DefaultInitializeOpts.SnapdSnapInfo,
	}
	err := s.Backend.Initialize(backendOpts)
	c.Assert(err, IsNil)

	devMode := interfaces.ConfinementOptions{DevMode: true}
	s.InstallSnap(c, devMode, "", ifacetest.SambaYamlV1, 1)

	profile := filepath.Join(dirs.SnapAppArmorDir, "snap.samba.smbd")

	checkProfileExtraRules(c, profile, expSnapConfineTransitionRules{
		usrBinSnapRules:   true,
		usrLibSnapdTarget: "/snap/snapd/321/usr/lib/snapd/snap-confine",
		snapdSnapTarget:   "/snap/snapd/321/usr/lib/snapd/snap-confine",
	})
}

func (s *backendSuite) TestInstallingDevmodeSnapBothSnapdAndCoreSnapOnlyExtraRulesCore(c *C) {
	r := release.MockOnClassic(false)
	defer r()

	// re-initialize with new options
	backendOpts := &interfaces.SecurityBackendOptions{
		SnapdSnapInfo: ifacetest.DefaultInitializeOpts.SnapdSnapInfo,
		CoreSnapInfo:  ifacetest.DefaultInitializeOpts.CoreSnapInfo,
	}
	err := s.Backend.Initialize(backendOpts)
	c.Assert(err, IsNil)

	devMode := interfaces.ConfinementOptions{DevMode: true}
	// snap base is core, but we are not on classic
	s.InstallSnap(c, devMode, "", ifacetest.SambaYamlV1, 1)

	profile := filepath.Join(dirs.SnapAppArmorDir, "snap.samba.smbd")
	checkProfileExtraRules(c, profile, expSnapConfineTransitionRules{
		usrBinSnapRules:   true,
		usrLibSnapdTarget: "/snap/snapd/321/usr/lib/snapd/snap-confine",
		snapdSnapTarget:   "/snap/snapd/321/usr/lib/snapd/snap-confine",
		coreSnapTarget:    "/snap/core/123/usr/lib/snapd/snap-confine",
	})
}

func (s *backendSuite) TestInstallingDevmodeSnapBothSnapdAndCoreSnapOnlyExtraRulesClassic(c *C) {
	r := release.MockOnClassic(true)
	defer r()

	// re-initialize with new options
	backendOpts := &interfaces.SecurityBackendOptions{
		SnapdSnapInfo: ifacetest.DefaultInitializeOpts.SnapdSnapInfo,
		CoreSnapInfo:  ifacetest.DefaultInitializeOpts.CoreSnapInfo,
	}
	err := s.Backend.Initialize(backendOpts)
	c.Assert(err, IsNil)

	devMode := interfaces.ConfinementOptions{DevMode: true}
	// base core snap
	s.InstallSnap(c, devMode, "", ifacetest.SambaYamlV1, 1)

	profile := filepath.Join(dirs.SnapAppArmorDir, "snap.samba.smbd")
	checkProfileExtraRules(c, profile, expSnapConfineTransitionRules{
		usrBinSnapRules:   true,
		usrLibSnapdTarget: "/snap/core/123/usr/lib/snapd/snap-confine",
		snapdSnapTarget:   "/snap/snapd/321/usr/lib/snapd/snap-confine",
		coreSnapTarget:    "/snap/core/123/usr/lib/snapd/snap-confine",
	})
}

func (s *backendSuite) TestInstallingDevmodeSnapNonCoreBaseBothSnapdAndCoreSnapOnlyExtraRulesClassic(c *C) {
	r := release.MockOnClassic(true)
	defer r()

	// re-initialize with new options
	backendOpts := &interfaces.SecurityBackendOptions{
		SnapdSnapInfo: ifacetest.DefaultInitializeOpts.SnapdSnapInfo,
		CoreSnapInfo:  ifacetest.DefaultInitializeOpts.CoreSnapInfo,
	}
	err := s.Backend.Initialize(backendOpts)
	c.Assert(err, IsNil)

	devMode := interfaces.ConfinementOptions{DevMode: true}
	// non-base core
	s.InstallSnap(c, devMode, "", ifacetest.SambaYamlV1Core20Base, 1)

	profile := filepath.Join(dirs.SnapAppArmorDir, "snap.samba.smbd")
	checkProfileExtraRules(c, profile, expSnapConfineTransitionRules{
		usrBinSnapRules:   true,
		usrLibSnapdTarget: "/snap/snapd/321/usr/lib/snapd/snap-confine",
		snapdSnapTarget:   "/snap/snapd/321/usr/lib/snapd/snap-confine",
		coreSnapTarget:    "/snap/core/123/usr/lib/snapd/snap-confine",
	})
}

func (s *backendSuite) TestInstallingDevmodeSnapNeitherSnapdNorCoreSnapInstalledPanicsLikeUC16InitialSeedWithDevmodeSnapInSeed(c *C) {
	r := release.MockOnClassic(true)
	defer r()

	// neither snap is installed
	backendOpts := &interfaces.SecurityBackendOptions{}
	err := s.Backend.Initialize(backendOpts)
	c.Assert(err, IsNil)

	devMode := interfaces.ConfinementOptions{DevMode: true}
	c.Assert(func() {
		s.InstallSnap(c, devMode, "", ifacetest.SambaYamlV1, 1)
	}, PanicMatches, "neither snapd nor core snap available while preparing apparmor profile for devmode snap samba, panicking to restart snapd to continue seeding")
}

func (s *backendSuite) TestInstallingSnapWritesAndLoadsProfiles(c *C) {
	s.InstallSnap(c, interfaces.ConfinementOptions{}, "", ifacetest.SambaYamlV1, 1)
	updateNSProfile := filepath.Join(dirs.SnapAppArmorDir, "snap-update-ns.samba")
	profile := filepath.Join(dirs.SnapAppArmorDir, "snap.samba.smbd")
	// file called "snap.sambda.smbd" was created
	_, err := os.Stat(profile)
	c.Check(err, IsNil)
	// apparmor_parser was used to load that file
	c.Check(s.loadProfilesCalls, DeepEquals, []loadProfilesParams{
		{[]string{updateNSProfile, profile}, fmt.Sprintf("%s/var/cache/apparmor", s.RootDir), apparmor_sandbox.SkipReadCache},
	})
}

func (s *backendSuite) TestInstallingSnapWithHookWritesAndLoadsProfiles(c *C) {
	s.InstallSnap(c, interfaces.ConfinementOptions{}, "", ifacetest.HookYaml, 1)
	profile := filepath.Join(dirs.SnapAppArmorDir, "snap.foo.hook.configure")
	updateNSProfile := filepath.Join(dirs.SnapAppArmorDir, "snap-update-ns.foo")

	// Verify that profile "snap.foo.hook.configure" was created
	_, err := os.Stat(profile)
	c.Check(err, IsNil)
	// apparmor_parser was used to load that file
	c.Check(s.loadProfilesCalls, DeepEquals, []loadProfilesParams{
		{[]string{updateNSProfile, profile}, fmt.Sprintf("%s/var/cache/apparmor", s.RootDir), apparmor_sandbox.SkipReadCache},
	})
}

const layoutYaml = `name: myapp
version: 1
apps:
  myapp:
    command: myapp
layout:
  /usr/share/myapp:
    bind: $SNAP/usr/share/myapp
`

func (s *backendSuite) TestInstallingSnapWithLayoutWritesAndLoadsProfiles(c *C) {
	s.InstallSnap(c, interfaces.ConfinementOptions{}, "", layoutYaml, 1)
	appProfile := filepath.Join(dirs.SnapAppArmorDir, "snap.myapp.myapp")
	updateNSProfile := filepath.Join(dirs.SnapAppArmorDir, "snap-update-ns.myapp")
	// both profiles were created
	_, err := os.Stat(appProfile)
	c.Check(err, IsNil)
	_, err = os.Stat(updateNSProfile)
	c.Check(err, IsNil)
	// TODO: check for layout snippets inside the generated file once we have some snippets to check for.
	// apparmor_parser was used to load them
	c.Check(s.loadProfilesCalls, DeepEquals, []loadProfilesParams{
		{[]string{updateNSProfile, appProfile}, fmt.Sprintf("%s/var/cache/apparmor", s.RootDir), apparmor_sandbox.SkipReadCache},
	})
}

const gadgetYaml = `name: mydevice
type: gadget
version: 1
`

func (s *backendSuite) TestInstallingSnapWithoutAppsOrHooksDoesntAddProfiles(c *C) {
	// Installing a snap that doesn't have either hooks or apps doesn't generate
	// any apparmor profiles because there is no executable content that would need
	// an execution environment and the corresponding mount namespace.
	s.InstallSnap(c, interfaces.ConfinementOptions{}, "", gadgetYaml, 1)
	c.Check(s.loadProfilesCalls, HasLen, 0)
}

func (s *backendSuite) TestTimings(c *C) {
	oldDurationThreshold := timings.DurationThreshold
	defer func() {
		timings.DurationThreshold = oldDurationThreshold
	}()
	timings.DurationThreshold = 0

	for _, opts := range testedConfinementOpts {
		perf := timings.New(nil)
		meas := perf.StartSpan("", "")

		snapInfo := s.InstallSnap(c, opts, "", ifacetest.SambaYamlV1, 1)
		appSet := interfaces.NewSnapAppSet(snapInfo)
		c.Assert(s.Backend.Setup(appSet, opts, s.Repo, meas), IsNil)

		st := state.New(nil)
		st.Lock()
		defer st.Unlock()
		perf.Save(st)

		var allTimings []map[string]interface{}
		c.Assert(st.Get("timings", &allTimings), IsNil)
		c.Assert(allTimings, HasLen, 1)

		timings, ok := allTimings[0]["timings"]
		c.Assert(ok, Equals, true)

		c.Assert(timings, HasLen, 2)
		timingsList, ok := timings.([]interface{})
		c.Assert(ok, Equals, true)
		tm := timingsList[0].(map[string]interface{})
		c.Check(tm["label"], Equals, "load-profiles[changed]")

		s.RemoveSnap(c, snapInfo)
	}
}

func (s *backendSuite) TestProfilesAreAlwaysLoaded(c *C) {
	for _, opts := range testedConfinementOpts {
		snapInfo := s.InstallSnap(c, opts, "", ifacetest.SambaYamlV1, 1)
		s.loadProfilesCalls = nil

		appSet := interfaces.NewSnapAppSet(snapInfo)
		err := s.Backend.Setup(appSet, opts, s.Repo, s.meas)
		c.Assert(err, IsNil)
		updateNSProfile := filepath.Join(dirs.SnapAppArmorDir, "snap-update-ns.samba")
		profile := filepath.Join(dirs.SnapAppArmorDir, "snap.samba.smbd")
		c.Check(s.loadProfilesCalls, DeepEquals, []loadProfilesParams{
			{[]string{updateNSProfile, profile}, fmt.Sprintf("%s/var/cache/apparmor", s.RootDir), 0},
		})
		s.RemoveSnap(c, snapInfo)
	}
}

func (s *backendSuite) TestRemovingSnapRemovesAndUnloadsProfiles(c *C) {
	for _, opts := range testedConfinementOpts {
		snapInfo := s.InstallSnap(c, opts, "", ifacetest.SambaYamlV1, 1)
		s.removeCachedProfilesCalls = nil
		s.RemoveSnap(c, snapInfo)
		c.Check(s.removeCachedProfilesCalls, DeepEquals, []removeCachedProfilesParams{
			{[]string{"snap-update-ns.samba", "snap.samba.smbd"}, fmt.Sprintf("%s/var/cache/apparmor", s.RootDir)},
		})
	}
}

func (s *backendSuite) TestRemovingSnapWithHookRemovesAndUnloadsProfiles(c *C) {
	for _, opts := range testedConfinementOpts {
		snapInfo := s.InstallSnap(c, opts, "", ifacetest.HookYaml, 1)
		s.removeCachedProfilesCalls = nil
		s.RemoveSnap(c, snapInfo)
		c.Check(s.removeCachedProfilesCalls, DeepEquals, []removeCachedProfilesParams{
			{[]string{"snap-update-ns.foo", "snap.foo.hook.configure"}, fmt.Sprintf("%s/var/cache/apparmor", s.RootDir)},
		})
	}
}

func (s *backendSuite) TestUpdatingSnapMakesNeccesaryChanges(c *C) {
	for _, opts := range testedConfinementOpts {
		snapInfo := s.InstallSnap(c, opts, "", ifacetest.SambaYamlV1, 1)
		s.loadProfilesCalls = nil
		snapInfo = s.UpdateSnap(c, snapInfo, opts, ifacetest.SambaYamlV1, 2)
		updateNSProfile := filepath.Join(dirs.SnapAppArmorDir, "snap-update-ns.samba")
		profile := filepath.Join(dirs.SnapAppArmorDir, "snap.samba.smbd")
		// apparmor_parser was used to reload the profile because snap revision
		// is inside the generated policy.
		c.Check(s.loadProfilesCalls, DeepEquals, []loadProfilesParams{
			{[]string{profile}, fmt.Sprintf("%s/var/cache/apparmor", s.RootDir), apparmor_sandbox.SkipReadCache},
			{[]string{updateNSProfile}, fmt.Sprintf("%s/var/cache/apparmor", s.RootDir), 0},
		})
		s.RemoveSnap(c, snapInfo)
	}
}

func (s *backendSuite) TestUpdatingSnapToOneWithMoreApps(c *C) {
	for _, opts := range testedConfinementOpts {
		snapInfo := s.InstallSnap(c, opts, "", ifacetest.SambaYamlV1, 1)
		s.loadProfilesCalls = nil
		// NOTE: the revision is kept the same to just test on the new application being added
		snapInfo = s.UpdateSnap(c, snapInfo, opts, ifacetest.SambaYamlV1WithNmbd, 1)
		updateNSProfile := filepath.Join(dirs.SnapAppArmorDir, "snap-update-ns.samba")
		smbdProfile := filepath.Join(dirs.SnapAppArmorDir, "snap.samba.smbd")
		nmbdProfile := filepath.Join(dirs.SnapAppArmorDir, "snap.samba.nmbd")
		// file called "snap.sambda.nmbd" was created
		_, err := os.Stat(nmbdProfile)
		c.Check(err, IsNil)
		// apparmor_parser was used to load all the profiles, the nmbd profile is new so we force invalidate its cache (if any).
		c.Check(s.loadProfilesCalls, DeepEquals, []loadProfilesParams{
			{[]string{nmbdProfile}, fmt.Sprintf("%s/var/cache/apparmor", s.RootDir), apparmor_sandbox.SkipReadCache},
			{[]string{updateNSProfile, smbdProfile}, fmt.Sprintf("%s/var/cache/apparmor", s.RootDir), 0},
		})
		s.RemoveSnap(c, snapInfo)
	}
}

func (s *backendSuite) TestUpdatingSnapToOneWithMoreHooks(c *C) {
	for _, opts := range testedConfinementOpts {
		snapInfo := s.InstallSnap(c, opts, "", ifacetest.SambaYamlV1WithNmbd, 1)
		s.loadProfilesCalls = nil
		// NOTE: the revision is kept the same to just test on the new application being added
		snapInfo = s.UpdateSnap(c, snapInfo, opts, ifacetest.SambaYamlWithHook, 1)
		updateNSProfile := filepath.Join(dirs.SnapAppArmorDir, "snap-update-ns.samba")
		smbdProfile := filepath.Join(dirs.SnapAppArmorDir, "snap.samba.smbd")
		nmbdProfile := filepath.Join(dirs.SnapAppArmorDir, "snap.samba.nmbd")
		hookProfile := filepath.Join(dirs.SnapAppArmorDir, "snap.samba.hook.configure")

		// Verify that profile "snap.samba.hook.configure" was created
		_, err := os.Stat(hookProfile)
		c.Check(err, IsNil)
		// apparmor_parser was used to load all the profiles, the hook profile has changed so we force invalidate its cache.
		c.Check(s.loadProfilesCalls, DeepEquals, []loadProfilesParams{
			{[]string{hookProfile}, fmt.Sprintf("%s/var/cache/apparmor", s.RootDir), apparmor_sandbox.SkipReadCache},
			{[]string{updateNSProfile, nmbdProfile, smbdProfile}, fmt.Sprintf("%s/var/cache/apparmor", s.RootDir), 0},
		})
		s.RemoveSnap(c, snapInfo)
	}
}

func (s *backendSuite) TestUpdatingSnapToOneWithFewerApps(c *C) {
	for _, opts := range testedConfinementOpts {
		snapInfo := s.InstallSnap(c, opts, "", ifacetest.SambaYamlV1WithNmbd, 1)
		s.loadProfilesCalls = nil
		// NOTE: the revision is kept the same to just test on the application being removed
		snapInfo = s.UpdateSnap(c, snapInfo, opts, ifacetest.SambaYamlV1, 1)
		updateNSProfile := filepath.Join(dirs.SnapAppArmorDir, "snap-update-ns.samba")
		smbdProfile := filepath.Join(dirs.SnapAppArmorDir, "snap.samba.smbd")
		nmbdProfile := filepath.Join(dirs.SnapAppArmorDir, "snap.samba.nmbd")
		// file called "snap.sambda.nmbd" was removed
		_, err := os.Stat(nmbdProfile)
		c.Check(os.IsNotExist(err), Equals, true)
		// apparmor_parser was used to remove the unused profile
		c.Check(s.loadProfilesCalls, DeepEquals, []loadProfilesParams{
			{[]string{updateNSProfile, smbdProfile}, fmt.Sprintf("%s/var/cache/apparmor", s.RootDir), 0},
		})
		s.RemoveSnap(c, snapInfo)
	}
}

func (s *backendSuite) TestUpdatingSnapToOneWithFewerHooks(c *C) {
	for _, opts := range testedConfinementOpts {
		snapInfo := s.InstallSnap(c, opts, "", ifacetest.SambaYamlWithHook, 1)
		s.loadProfilesCalls = nil
		// NOTE: the revision is kept the same to just test on the application being removed
		snapInfo = s.UpdateSnap(c, snapInfo, opts, ifacetest.SambaYamlV1WithNmbd, 1)
		updateNSProfile := filepath.Join(dirs.SnapAppArmorDir, "snap-update-ns.samba")
		smbdProfile := filepath.Join(dirs.SnapAppArmorDir, "snap.samba.smbd")
		nmbdProfile := filepath.Join(dirs.SnapAppArmorDir, "snap.samba.nmbd")
		hookProfile := filepath.Join(dirs.SnapAppArmorDir, "snap.samba.hook.configure")

		// Verify profile "snap.samba.hook.configure" was removed
		_, err := os.Stat(hookProfile)
		c.Check(os.IsNotExist(err), Equals, true)
		// apparmor_parser was used to remove the unused profile
		c.Check(s.loadProfilesCalls, DeepEquals, []loadProfilesParams{
			{[]string{updateNSProfile, nmbdProfile, smbdProfile}, fmt.Sprintf("%s/var/cache/apparmor", s.RootDir), 0},
		})
		s.RemoveSnap(c, snapInfo)
	}
}

// SetupMany tests

func (s *backendSuite) TestSetupManyProfilesAreAlwaysLoaded(c *C) {
	for _, opts := range testedConfinementOpts {
		snapInfo1 := s.InstallSnap(c, opts, "", ifacetest.SambaYamlV1, 1)
		snapInfo2 := s.InstallSnap(c, opts, "", ifacetest.SomeSnapYamlV1, 1)
		appSet1 := interfaces.NewSnapAppSet(snapInfo1)
		appSet2 := interfaces.NewSnapAppSet(snapInfo2)
		s.loadProfilesCalls = nil
		setupManyInterface, ok := s.Backend.(interfaces.SecurityBackendSetupMany)
		c.Assert(ok, Equals, true)
		err := setupManyInterface.SetupMany([]*interfaces.SnapAppSet{appSet1, appSet2}, func(snapName string) interfaces.ConfinementOptions { return opts }, s.Repo, s.meas)
		c.Assert(err, IsNil)
		snap1nsProfile := filepath.Join(dirs.SnapAppArmorDir, "snap-update-ns.samba")
		snap1AAprofile := filepath.Join(dirs.SnapAppArmorDir, "snap.samba.smbd")
		snap2nsProfile := filepath.Join(dirs.SnapAppArmorDir, "snap-update-ns.some-snap")
		snap2AAprofile := filepath.Join(dirs.SnapAppArmorDir, "snap.some-snap.someapp")
		c.Check(s.loadProfilesCalls, DeepEquals, []loadProfilesParams{
			{[]string{snap1nsProfile, snap1AAprofile, snap2nsProfile, snap2AAprofile}, fmt.Sprintf("%s/var/cache/apparmor", s.RootDir), apparmor_sandbox.ConserveCPU},
		})
		s.RemoveSnap(c, snapInfo1)
		s.RemoveSnap(c, snapInfo2)
	}
}

func (s *backendSuite) TestSetupManyProfilesWithChanged(c *C) {
	for _, opts := range testedConfinementOpts {
		snapInfo1 := s.InstallSnap(c, opts, "", ifacetest.SambaYamlV1, 1)
		snapInfo2 := s.InstallSnap(c, opts, "", ifacetest.SomeSnapYamlV1, 1)
		appSet1 := interfaces.NewSnapAppSet(snapInfo1)
		appSet2 := interfaces.NewSnapAppSet(snapInfo2)
		s.loadProfilesCalls = nil

		snap1nsProfile := filepath.Join(dirs.SnapAppArmorDir, "snap-update-ns.samba")
		snap1AAprofile := filepath.Join(dirs.SnapAppArmorDir, "snap.samba.smbd")
		snap2nsProfile := filepath.Join(dirs.SnapAppArmorDir, "snap-update-ns.some-snap")
		snap2AAprofile := filepath.Join(dirs.SnapAppArmorDir, "snap.some-snap.someapp")

		// simulate outdated profiles by changing their data on the disk
		c.Assert(os.WriteFile(snap1AAprofile, []byte("# an outdated profile"), 0644), IsNil)
		c.Assert(os.WriteFile(snap2AAprofile, []byte("# an outdated profile"), 0644), IsNil)

		setupManyInterface, ok := s.Backend.(interfaces.SecurityBackendSetupMany)
		c.Assert(ok, Equals, true)
		err := setupManyInterface.SetupMany([]*interfaces.SnapAppSet{appSet1, appSet2}, func(snapName string) interfaces.ConfinementOptions { return opts }, s.Repo, s.meas)
		c.Assert(err, IsNil)

		// expect two batch executions - one for changed profiles, second for unchanged profiles.
		c.Check(s.loadProfilesCalls, DeepEquals, []loadProfilesParams{
			{[]string{snap1AAprofile, snap2AAprofile}, fmt.Sprintf("%s/var/cache/apparmor", s.RootDir), apparmor_sandbox.SkipReadCache | apparmor_sandbox.ConserveCPU},
			{[]string{snap1nsProfile, snap2nsProfile}, fmt.Sprintf("%s/var/cache/apparmor", s.RootDir), apparmor_sandbox.ConserveCPU},
		})
		s.RemoveSnap(c, snapInfo1)
		s.RemoveSnap(c, snapInfo2)
	}
}

// helper for checking for apparmor parser calls where batch run is expected to fail and is followed by two separate runs for individual snaps.
func (s *backendSuite) checkSetupManyCallsWithFallback(c *C, invocations []loadProfilesParams) {
	snap1nsProfile := filepath.Join(dirs.SnapAppArmorDir, "snap-update-ns.samba")
	snap1AAprofile := filepath.Join(dirs.SnapAppArmorDir, "snap.samba.smbd")
	snap2nsProfile := filepath.Join(dirs.SnapAppArmorDir, "snap-update-ns.some-snap")
	snap2AAprofile := filepath.Join(dirs.SnapAppArmorDir, "snap.some-snap.someapp")

	// We expect three calls to apparmor_parser due to the failure of batch run. First is the failed batch run, followed by succesfull fallback runs.
	c.Check(invocations, DeepEquals, []loadProfilesParams{
		{[]string{snap1nsProfile, snap1AAprofile, snap2nsProfile, snap2AAprofile}, fmt.Sprintf("%s/var/cache/apparmor", s.RootDir), apparmor_sandbox.ConserveCPU},
		{[]string{snap1nsProfile, snap1AAprofile}, fmt.Sprintf("%s/var/cache/apparmor", s.RootDir), 0},
		{[]string{snap2nsProfile, snap2AAprofile}, fmt.Sprintf("%s/var/cache/apparmor", s.RootDir), 0},
	})
}

func (s *backendSuite) TestSetupManyApparmorBatchProcessingPermanentError(c *C) {
	log, restore := logger.MockLogger()
	defer restore()

	for _, opts := range testedConfinementOpts {
		log.Reset()

		// note, InstallSnap here uses s.parserCmd which mocks happy apparmor_parser
		snapInfo1 := s.InstallSnap(c, opts, "", ifacetest.SambaYamlV1, 1)
		snapInfo2 := s.InstallSnap(c, opts, "", ifacetest.SomeSnapYamlV1, 1)
		appSet1 := interfaces.NewSnapAppSet(snapInfo1)
		appSet2 := interfaces.NewSnapAppSet(snapInfo2)
		s.loadProfilesCalls = nil
		setupManyInterface, ok := s.Backend.(interfaces.SecurityBackendSetupMany)
		c.Assert(ok, Equals, true)

		// mock apparmor_parser again with a failing one (and restore immediately for the next iteration of the test)
		s.loadProfilesReturn = errors.New("apparmor_parser crash")
		errs := setupManyInterface.SetupMany([]*interfaces.SnapAppSet{appSet1, appSet2}, func(snapName string) interfaces.ConfinementOptions { return opts }, s.Repo, s.meas)
		s.loadProfilesReturn = nil

		s.checkSetupManyCallsWithFallback(c, s.loadProfilesCalls)

		// two errors expected: SetupMany failure on multiple snaps falls back to one-by-one apparmor invocations. Both fail on apparmor_parser again and we only see
		// individual failures. Error from batch run is only logged.
		c.Assert(errs, HasLen, 2)
		c.Check(errs[0], ErrorMatches, ".*cannot setup profiles for snap \"samba\": apparmor_parser crash")
		c.Check(errs[1], ErrorMatches, ".*cannot setup profiles for snap \"some-snap\": apparmor_parser crash")
		c.Check(log.String(), Matches, ".*failed to batch-reload unchanged profiles: apparmor_parser crash\n")

		s.RemoveSnap(c, snapInfo1)
		s.RemoveSnap(c, snapInfo2)
	}
}

func (s *backendSuite) TestSetupManyApparmorBatchProcessingErrorWithFallbackOK(c *C) {
	log, restore := logger.MockLogger()
	defer restore()

	for _, opts := range testedConfinementOpts {
		log.Reset()

		// note, InstallSnap here uses s.parserCmd which mocks happy apparmor_parser
		snapInfo1 := s.InstallSnap(c, opts, "", ifacetest.SambaYamlV1, 1)
		snapInfo2 := s.InstallSnap(c, opts, "", ifacetest.SomeSnapYamlV1, 1)
		appSet1 := interfaces.NewSnapAppSet(snapInfo1)
		appSet2 := interfaces.NewSnapAppSet(snapInfo2)
		s.loadProfilesCalls = nil
		setupManyInterface, ok := s.Backend.(interfaces.SecurityBackendSetupMany)
		c.Assert(ok, Equals, true)

		// mock apparmor_parser again with a failing one (and restore immediately for the next iteration of the test)
		r := apparmor.MockLoadProfiles(func(fnames []string, cacheDir string, flags apparmor_sandbox.AaParserFlags) error {
			if len(fnames) == 0 {
				return nil
			}
			s.loadProfilesCalls = append(s.loadProfilesCalls, loadProfilesParams{fnames, cacheDir, flags})
			if len(fnames) > 3 {
				return errors.New("some error")
			}
			return nil
		})
		errs := setupManyInterface.SetupMany([]*interfaces.SnapAppSet{appSet1, appSet2}, func(snapName string) interfaces.ConfinementOptions { return opts }, s.Repo, s.meas)
		r()

		s.checkSetupManyCallsWithFallback(c, s.loadProfilesCalls)

		// no errors expected: error from batch run is only logged, but individual apparmor parser execution as part of the fallback are successful.
		// note, tnis scenario is unlikely to happen in real life, because if a profile failed in a batch, it would fail when parsed alone too. It is
		// tested here just to exercise various execution paths.
		c.Assert(errs, HasLen, 0)
		c.Check(log.String(), Matches, ".*failed to batch-reload unchanged profiles: some error\n")

		s.RemoveSnap(c, snapInfo1)
		s.RemoveSnap(c, snapInfo2)
	}
}

func (s *backendSuite) TestSetupManyApparmorBatchProcessingErrorWithFallbackPartiallyOK(c *C) {
	log, restore := logger.MockLogger()
	defer restore()

	for _, opts := range testedConfinementOpts {
		log.Reset()

		// note, InstallSnap here uses s.parserCmd which mocks happy apparmor_parser
		snapInfo1 := s.InstallSnap(c, opts, "", ifacetest.SambaYamlV1, 1)
		snapInfo2 := s.InstallSnap(c, opts, "", ifacetest.SomeSnapYamlV1, 1)
		appSet1 := interfaces.NewSnapAppSet(snapInfo1)
		appSet2 := interfaces.NewSnapAppSet(snapInfo2)
		s.loadProfilesCalls = nil
		setupManyInterface, ok := s.Backend.(interfaces.SecurityBackendSetupMany)
		c.Assert(ok, Equals, true)

		// mock apparmor_parser with a failing one
		r := apparmor.MockLoadProfiles(func(fnames []string, cacheDir string, flags apparmor_sandbox.AaParserFlags) error {
			if len(fnames) == 0 {
				return nil
			}
			s.loadProfilesCalls = append(s.loadProfilesCalls, loadProfilesParams{fnames, cacheDir, flags})
			// If the profile list contains SAMBA, we fail
			for _, profilePath := range fnames {
				name := filepath.Base(profilePath)
				if name == "snap.samba.smbd" {
					return errors.New("fail on samba")
				}
			}
			return nil
		})
		errs := setupManyInterface.SetupMany([]*interfaces.SnapAppSet{appSet1, appSet2}, func(snapName string) interfaces.ConfinementOptions { return opts }, s.Repo, s.meas)
		r()

		s.checkSetupManyCallsWithFallback(c, s.loadProfilesCalls)

		// the batch reload fails because of snap.samba.smbd profile failing
		c.Check(log.String(), Matches, ".* failed to batch-reload unchanged profiles: fail on samba\n")
		// and we also fail when running that profile in fallback mode
		c.Assert(errs, HasLen, 1)
		c.Assert(errs[0], ErrorMatches, "cannot setup profiles for snap \"samba\": fail on samba")

		s.RemoveSnap(c, snapInfo1)
		s.RemoveSnap(c, snapInfo2)
	}
}

const snapcraftPrYaml = `name: snapcraft-pr
version: 1
apps:
  snapcraft-pr:
    cmd: snapcraft-pr
`

const snapcraftYaml = `name: snapcraft
version: 1
apps:
  snapcraft:
    cmd: snapcraft
`

func (s *backendSuite) TestInstallingSnapDoesntBreakSnapsWithPrefixName(c *C) {
	snapcraftProfile := filepath.Join(dirs.SnapAppArmorDir, "snap.snapcraft.snapcraft")
	snapcraftPrProfile := filepath.Join(dirs.SnapAppArmorDir, "snap.snapcraft-pr.snapcraft-pr")
	// Install snapcraft-pr and check that its profile was created.
	s.InstallSnap(c, interfaces.ConfinementOptions{}, "", snapcraftPrYaml, 1)
	_, err := os.Stat(snapcraftPrProfile)
	c.Check(err, IsNil)

	// Install snapcraft (sans the -pr suffix) and check that its profile was created.
	// Check that this didn't remove the profile of snapcraft-pr installed earlier.
	s.InstallSnap(c, interfaces.ConfinementOptions{}, "", snapcraftYaml, 1)
	_, err = os.Stat(snapcraftProfile)
	c.Check(err, IsNil)
	_, err = os.Stat(snapcraftPrProfile)
	c.Check(err, IsNil)
}

func (s *backendSuite) TestRemovingSnapDoesntBreakSnapsWIthPrefixName(c *C) {
	snapcraftProfile := filepath.Join(dirs.SnapAppArmorDir, "snap.snapcraft.snapcraft")
	snapcraftPrProfile := filepath.Join(dirs.SnapAppArmorDir, "snap.snapcraft-pr.snapcraft-pr")

	// Install snapcraft-pr and check that its profile was created.
	s.InstallSnap(c, interfaces.ConfinementOptions{}, "", snapcraftPrYaml, 1)
	_, err := os.Stat(snapcraftPrProfile)
	c.Check(err, IsNil)

	// Install snapcraft (sans the -pr suffix) and check that its profile was created.
	// Check that this didn't remove the profile of snapcraft-pr installed earlier.
	snapInfo := s.InstallSnap(c, interfaces.ConfinementOptions{}, "", snapcraftYaml, 1)
	_, err = os.Stat(snapcraftProfile)
	c.Check(err, IsNil)
	_, err = os.Stat(snapcraftPrProfile)
	c.Check(err, IsNil)

	// Remove snapcraft (sans the -pr suffix) and check that its profile was removed.
	// Check that this didn't remove the profile of snapcraft-pr installed earlier.
	s.RemoveSnap(c, snapInfo)
	_, err = os.Stat(snapcraftProfile)
	c.Check(os.IsNotExist(err), Equals, true)
	_, err = os.Stat(snapcraftPrProfile)
	c.Check(err, IsNil)
}

func (s *backendSuite) TestDefaultCoreRuntimesTemplateOnlyUsed(c *C) {
	for _, base := range []string{
		"",
		"base: core16",
		"base: core18",
		"base: core20",
		"base: core22",
		"base: core98",
	} {
		restore := apparmor_sandbox.MockLevel(apparmor_sandbox.Full)
		defer restore()

		testYaml := ifacetest.SambaYamlV1 + base + "\n"

		snapInfo := snaptest.MockInfo(c, testYaml, nil)
		appSet := interfaces.NewSnapAppSet(snapInfo)
		// NOTE: we don't call apparmor.MockTemplate()
		err := s.Backend.Setup(appSet, interfaces.ConfinementOptions{}, s.Repo, s.meas)
		c.Assert(err, IsNil)
		profile := filepath.Join(dirs.SnapAppArmorDir, "snap.samba.smbd")
		data, err := ioutil.ReadFile(profile)
		c.Assert(err, IsNil)
		for _, line := range []string{
			// preamble
			"#include <tunables/global>\n",
			// footer
			"}\n",
			// templateCommon
			"/etc/ld.so.preload r,\n",
			"owner @{PROC}/@{pid}/maps k,\n",
			"/tmp/   r,\n",
			"/sys/class/ r,\n",
			// defaultCoreRuntimeTemplateRules
			"# Default rules for core base runtimes\n",
			"/usr/share/terminfo/** k,\n",
		} {
			c.Assert(string(data), testutil.Contains, line)
		}
		for _, line := range []string{
			// defaultOtherBaseTemplateRules should not be present
			"# Default rules for non-core base runtimes\n",
			"/{,s}bin/** mrklix,\n",
		} {
			c.Assert(string(data), Not(testutil.Contains), line)
		}
	}
}

func (s *backendSuite) TestBaseDefaultTemplateOnlyUsed(c *C) {
	restore := apparmor_sandbox.MockLevel(apparmor_sandbox.Full)
	defer restore()

	testYaml := ifacetest.SambaYamlV1 + "base: other\n"

	snapInfo := snaptest.MockInfo(c, testYaml, nil)
	appSet := interfaces.NewSnapAppSet(snapInfo)
	// NOTE: we don't call apparmor.MockTemplate()
	err := s.Backend.Setup(appSet, interfaces.ConfinementOptions{}, s.Repo, s.meas)
	c.Assert(err, IsNil)
	profile := filepath.Join(dirs.SnapAppArmorDir, "snap.samba.smbd")
	data, err := ioutil.ReadFile(profile)
	c.Assert(err, IsNil)
	for _, line := range []string{
		// preamble
		"#include <tunables/global>\n",
		// footer
		"}\n",
		// templateCommon
		"/etc/ld.so.preload r,\n",
		"owner @{PROC}/@{pid}/maps k,\n",
		"/tmp/   r,\n",
		"/sys/class/ r,\n",
		// defaultOtherBaseTemplateRules
		"# Default rules for non-core base runtimes\n",
		"/{,s}bin/** mrklix,\n",
	} {
		c.Assert(string(data), testutil.Contains, line)
	}
	for _, line := range []string{
		// defaultCoreRuntimeTemplateRules should not be present
		"# Default rules for core base runtimes\n",
		"/usr/share/terminfo/** k,\n",
		"/{,usr/}bin/arch ixr,\n",
	} {
		c.Assert(string(data), Not(testutil.Contains), line)
	}
}

func (s *backendSuite) TestTemplateRulesInCommon(c *C) {
	// assume that we lstrip() the line
	commonFiles := regexp.MustCompile(`^(audit +)?(deny +)?(owner +)?/((dev|etc|run|sys|tmp|{dev,run}|{,var/}run|usr/lib/snapd|var/lib/extrausers|var/lib/snapd)/|var/snap/{?@{SNAP_)`)
	commonFilesVar := regexp.MustCompile(`^(audit +)?(deny +)?(owner +)?@{(HOME|HOMEDIRS|INSTALL_DIR|PROC)}/`)
	commonOther := regexp.MustCompile(`^([^/@#]|#include +<)`)

	// first, verify the regexes themselves

	// Expected matches
	for idx, tc := range []string{
		// abstraction
		"#include <abstractions/base>",
		// file
		"/dev/{,u}random w,",
		"/dev/{,u}random w, # test comment",
		"/{dev,run}/shm/snap.@{SNAP_INSTANCE_NAME}.** mrwlkix,",
		"/etc/ld.so.preload r,",
		"@{INSTALL_DIR}/{@{SNAP_NAME},@{SNAP_INSTANCE_NAME}}/ r,",
		"deny @{INSTALL_DIR}/{@{SNAP_NAME},@{SNAP_INSTANCE_NAME}}/**/__pycache__/*.pyc.[0-9]* w,",
		"audit /dev/something r,",
		"audit deny /dev/something r,",
		"audit deny owner /dev/something r,",
		"@{PROC}/ r,",
		"owner @{PROC}/@{pid}/{,task/@{tid}}fd/[0-9]* rw,",
		"/run/uuidd/request rw,",
		"owner /run/user/[0-9]*/snap.@{SNAP_INSTANCE_NAME}/   rw,",
		"/sys/devices/virtual/tty/{console,tty*}/active r,",
		"/tmp/   r,",
		"/{,var/}run/udev/tags/snappy-assign/ r,",
		"/usr/lib/snapd/foo r,",
		"/var/lib/extrausers/foo r,",
		"/var/lib/snapd/foo r,",
		"/var/snap/{@{SNAP_NAME},@{SNAP_INSTANCE_NAME}}/ r",
		"/var/snap/@{SNAP_NAME}/ r",
		// capability
		"capability ipc_lock,",
		// dbus - single line
		"dbus (receive, send) peer=(label=snap.@{SNAP_INSTANCE_NAME}.*),",
		// dbus - multiline
		"dbus (send)",
		"bus={session,system}",
		"path=/org/freedesktop/DBus",
		"interface=org.freedesktop.DBus.Introspectable",
		"member=Introspect",
		"peer=(label=unconfined),",
		// mount
		"mount,",
		"remount,",
		"umount,",
		// network
		"network,",
		// pivot_root
		"pivot_root,",
		// ptrace
		"ptrace,",
		// signal
		"signal peer=snap.@{SNAP_INSTANCE_NAME}.*,",
		// unix
		"unix peer=(label=snap.@{SNAP_INSTANCE_NAME}.*),",
	} {
		c.Logf("trying %d: %s", idx, tc)
		cf := commonFiles.MatchString(tc)
		cfv := commonFilesVar.MatchString(tc)
		co := commonOther.MatchString(tc)
		c.Check(cf || cfv || co, Equals, true)
	}

	// Expected no matches
	for idx, tc := range []string{
		"/bin/ls",
		"# some comment",
		"deny /usr/lib/python3*/{,**/}__pycache__/ w,",
	} {
		c.Logf("trying %d: %s", idx, tc)
		cf := commonFiles.MatchString(tc)
		cfv := commonFilesVar.MatchString(tc)
		co := commonOther.MatchString(tc)
		c.Check(cf && cfv && co, Equals, false)
	}

	for _, raw := range strings.Split(apparmor.DefaultCoreRuntimeTemplateRules, "\n") {
		line := strings.TrimLeft(raw, " \t")
		cf := commonFiles.MatchString(line)
		cfv := commonFilesVar.MatchString(line)
		co := commonOther.MatchString(line)
		res := cf || cfv || co
		if res {
			c.Logf("ERROR: found rule that should be in templateCommon (default template rules): %s", line)
		}
		c.Check(res, Equals, false)
	}

	for _, raw := range strings.Split(apparmor.DefaultOtherBaseTemplateRules, "\n") {
		line := strings.TrimLeft(raw, " \t")
		cf := commonFiles.MatchString(line)
		cfv := commonFilesVar.MatchString(line)
		co := commonOther.MatchString(line)
		res := cf || cfv || co
		if res {
			c.Logf("ERROR: found rule that should be in templateCommon (default base template rules): %s", line)
		}
		c.Check(res, Equals, false)
	}
}

type combineSnippetsScenario struct {
	opts    interfaces.ConfinementOptions
	snippet string
	content string
}

const commonPrefix = `
# This is a snap name without the instance key
@{SNAP_NAME}="samba"
# This is a snap name with instance key
@{SNAP_INSTANCE_NAME}="samba"
@{SNAP_INSTANCE_DESKTOP}="samba"
@{SNAP_COMMAND_NAME}="smbd"
@{SNAP_REVISION}="1"
@{PROFILE_DBUS}="snap_2esamba_2esmbd"
@{INSTALL_DIR}="/{,var/lib/snapd/}snap"`

var combineSnippetsScenarios = []combineSnippetsScenario{{
	// By default apparmor is enforcing mode.
	opts:    interfaces.ConfinementOptions{},
	content: commonPrefix + "\nprofile \"snap.samba.smbd\" flags=(attach_disconnected,mediate_deleted) {\n\n}\n",
}, {
	// Snippets are injected in the space between "{" and "}"
	opts:    interfaces.ConfinementOptions{},
	snippet: "snippet",
	content: commonPrefix + "\nprofile \"snap.samba.smbd\" flags=(attach_disconnected,mediate_deleted) {\nsnippet\n}\n",
}, {
	// DevMode switches apparmor to non-enforcing (complain) mode.
	opts:    interfaces.ConfinementOptions{DevMode: true},
	snippet: "snippet",
	content: commonPrefix + "\nprofile \"snap.samba.smbd\" flags=(attach_disconnected,mediate_deleted,complain) {\nsnippet\n}\n",
}, {
	// JailMode switches apparmor to enforcing mode even in the presence of DevMode.
	opts:    interfaces.ConfinementOptions{DevMode: true},
	snippet: "snippet",
	content: commonPrefix + "\nprofile \"snap.samba.smbd\" flags=(attach_disconnected,mediate_deleted,complain) {\nsnippet\n}\n",
}, {
	// Classic confinement (without jailmode) uses apparmor in complain mode by default and ignores all snippets.
	opts:    interfaces.ConfinementOptions{Classic: true},
	snippet: "snippet",
	content: "\n#classic" + commonPrefix + "\nprofile \"snap.samba.smbd\" flags=(attach_disconnected,mediate_deleted,complain) {\n\n}\n",
}, {
	// Classic confinement in JailMode uses enforcing apparmor.
	opts:    interfaces.ConfinementOptions{Classic: true, JailMode: true},
	snippet: "snippet",
	content: commonPrefix + `
profile "snap.samba.smbd" flags=(attach_disconnected,mediate_deleted) {

  # Read-only access to the core snap.
  @{INSTALL_DIR}/core/** r,
  # Read only access to the core snap to load libc from.
  # This is related to LP: #1666897
  @{INSTALL_DIR}/core/*/{,usr/}lib/@{multiarch}/{,**/}lib*.so* m,

  # For snappy reexec on 4.8+ kernels
  @{INSTALL_DIR}/core/*/usr/lib/snapd/snap-exec m,

snippet
}
`,
}}

func (s *backendSuite) TestCombineSnippets(c *C) {
	restore := apparmor_sandbox.MockLevel(apparmor_sandbox.Full)
	defer restore()
	restore = osutil.MockIsHomeUsingNFS(func() (bool, error) { return false, nil })
	defer restore()
	restore = osutil.MockIsRootWritableOverlay(func() (string, error) { return "", nil })
	defer restore()

	// NOTE: replace the real template with a shorter variant
	restoreTemplate := apparmor.MockTemplate("\n" +
		"###VAR###\n" +
		"###PROFILEATTACH### ###FLAGS### {\n" +
		"###SNIPPETS###\n" +
		"}\n")
	defer restoreTemplate()
	restoreClassicTemplate := apparmor.MockClassicTemplate("\n" +
		"#classic\n" +
		"###VAR###\n" +
		"###PROFILEATTACH### ###FLAGS### {\n" +
		"###SNIPPETS###\n" +
		"}\n")
	defer restoreClassicTemplate()
	for i, scenario := range combineSnippetsScenarios {
		s.Iface.AppArmorPermanentSlotCallback = func(spec *apparmor.Specification, slot *snap.SlotInfo) error {
			if scenario.snippet == "" {
				return nil
			}
			spec.AddSnippet(scenario.snippet)
			return nil
		}
		snapInfo := s.InstallSnap(c, scenario.opts, "", ifacetest.SambaYamlV1, 1)
		profile := filepath.Join(dirs.SnapAppArmorDir, "snap.samba.smbd")
		c.Check(profile, testutil.FileEquals, scenario.content, Commentf("scenario %d: %#v", i, scenario))
		stat, err := os.Stat(profile)
		c.Assert(err, IsNil)
		c.Check(stat.Mode(), Equals, os.FileMode(0644))
		s.RemoveSnap(c, snapInfo)
	}
}

func (s *backendSuite) TestUnconfinedFlag(c *C) {
	restore := apparmor_sandbox.MockLevel(apparmor_sandbox.Full)
	defer restore()
	restore = osutil.MockIsHomeUsingNFS(func() (bool, error) { return false, nil })
	defer restore()
	restore = osutil.MockIsRootWritableOverlay(func() (string, error) { return "", nil })
	defer restore()

	// NOTE: replace the real template with a shorter variant
	restoreTemplate := apparmor.MockTemplate("\n" +
		"###VAR###\n" +
		"###PROFILEATTACH### ###FLAGS### {\n" +
		"###SNIPPETS###\n" +
		"}\n")
	defer restoreTemplate()
	restoreClassicTemplate := apparmor.MockClassicTemplate("\n" +
		"#classic\n" +
		"###VAR###\n" +
		"###PROFILEATTACH### ###FLAGS### {\n" +
		"###SNIPPETS###\n" +
		"}\n")
	defer restoreClassicTemplate()
	s.Iface.InterfaceStaticInfo.AppArmorUnconfinedSlots = true
	// will only be enabled if the interface also enables unconfined
	s.Iface.AppArmorPermanentSlotCallback = func(spec *apparmor.Specification, slot *snap.SlotInfo) error {
		err := spec.SetUnconfinedEnabled()
		c.Assert(err, IsNil)
		return nil
	}
	// test both classic and non-classic confinement
	options := []interfaces.ConfinementOptions{
		{},
		{Classic: true},
	}
	restore = apparmor.MockParserFeatures(func() ([]string, error) { return []string{"unconfined"}, nil })
	defer restore()
	restore = apparmor.MockKernelFeatures(func() ([]string, error) { return []string{"policy:unconfined_restrictions"}, nil })
	defer restore()

	for i, opts := range options {
		snapInfo := s.InstallSnap(c, opts, "", ifacetest.SambaYamlV1, 1)
		profile := filepath.Join(dirs.SnapAppArmorDir, "snap.samba.smbd")
		flags := []string{"attach_disconnected", "mediate_deleted", "unconfined"}
		prefix := commonPrefix
		if opts.Classic {
			prefix = "\n#classic" + commonPrefix
		}
		contents := fmt.Sprintf(prefix+"\nprofile \"snap.samba.smbd\" flags=(%s) {\n\n}\n",
			strings.Join(flags, ","))
		c.Check(profile, testutil.FileEquals, contents, Commentf("scenario %d: %#v", i, opts))
		stat, err := os.Stat(profile)
		c.Assert(err, IsNil)
		c.Check(stat.Mode(), Equals, os.FileMode(0644))
		s.RemoveSnap(c, snapInfo)

	}
}

func (s *backendSuite) TestCombineSnippetsChangeProfile(c *C) {
	restore := apparmor_sandbox.MockLevel(apparmor_sandbox.Full)
	defer restore()
	restore = osutil.MockIsHomeUsingNFS(func() (bool, error) { return false, nil })
	defer restore()
	restore = osutil.MockIsRootWritableOverlay(func() (string, error) { return "", nil })
	defer restore()

	restoreClassicTemplate := apparmor.MockClassicTemplate("###CHANGEPROFILE_RULE###")
	defer restoreClassicTemplate()

	type changeProfileScenario struct {
		features []string
		expected string
	}

	var changeProfileScenarios = []changeProfileScenario{{
		features: []string{},
		expected: "change_profile,",
	}, {
		features: []string{"unsafe"},
		expected: "change_profile unsafe /**,",
	}}

	for i, scenario := range changeProfileScenarios {
		restore = apparmor.MockParserFeatures(func() ([]string, error) { return scenario.features, nil })
		defer restore()

		snapInfo := s.InstallSnap(c, interfaces.ConfinementOptions{Classic: true}, "", ifacetest.SambaYamlV1, 1)
		profile := filepath.Join(dirs.SnapAppArmorDir, "snap.samba.smbd")
		c.Check(profile, testutil.FileEquals, scenario.expected, Commentf("scenario %d: %#v", i, scenario))
		stat, err := os.Stat(profile)
		c.Assert(err, IsNil)
		c.Check(stat.Mode(), Equals, os.FileMode(0644))
		s.RemoveSnap(c, snapInfo)
	}
}

func (s *backendSuite) TestCombineSnippetsIncludeIfExistsSnapTuning(c *C) {
	restore := apparmor_sandbox.MockLevel(apparmor_sandbox.Full)
	defer restore()
	restore = osutil.MockIsHomeUsingNFS(func() (bool, error) { return false, nil })
	defer restore()
	restore = osutil.MockIsRootWritableOverlay(func() (string, error) { return "", nil })
	defer restore()

	restoreTemplate := apparmor.MockTemplate("###INCLUDE_IF_EXISTS_SNAP_TUNING###")
	defer restoreTemplate()

	type includeIfExistsScenario struct {
		features []string
		expected string
	}

	var includeIfExistsScenarios = []includeIfExistsScenario{{
		features: []string{},
		expected: "",
	}, {
		features: []string{"include-if-exists"},
		expected: `#include if exists "/var/lib/snapd/apparmor/snap-tuning"`,
	}}

	for i, scenario := range includeIfExistsScenarios {
		restore = apparmor.MockParserFeatures(func() ([]string, error) { return scenario.features, nil })
		defer restore()

		snapInfo := s.InstallSnap(c, interfaces.ConfinementOptions{}, "", ifacetest.SambaYamlV1, 1)
		profile := filepath.Join(dirs.SnapAppArmorDir, "snap.samba.smbd")
		c.Check(profile, testutil.FileEquals, scenario.expected, Commentf("scenario %d: %#v", i, scenario))
		stat, err := os.Stat(profile)
		c.Assert(err, IsNil)
		c.Check(stat.Mode(), Equals, os.FileMode(0644))
		s.RemoveSnap(c, snapInfo)
	}
}

func (s *backendSuite) TestCombineSnippetsIncludeIfExistsLocalSnapProfile(c *C) {
	restore := apparmor_sandbox.MockLevel(apparmor_sandbox.Full)
	defer restore()
	restore = osutil.MockIsHomeUsingNFS(func() (bool, error) { return false, nil })
	defer restore()
	restore = osutil.MockIsRootWritableOverlay(func() (string, error) { return "", nil })
	defer restore()

	restoreTemplate := apparmor.MockTemplate("###INCLUDE_IF_EXISTS_LOCAL_SNAP_PROFILE###")
	defer restoreTemplate()

	type includeIfExistsScenario struct {
		features []string
		expected string
	}

	var includeIfExistsScenarios = []includeIfExistsScenario{{
		features: []string{},
		expected: "",
	}, {
		features: []string{"include-if-exists"},
		expected: fmt.Sprintf("#include if exists \"%s\"",
			filepath.Join(dirs.SnapAppArmorDir, "local", "snap.samba.smbd")),
	}}

	for i, scenario := range includeIfExistsScenarios {
		restore = apparmor.MockParserFeatures(func() ([]string, error) { return scenario.features, nil })
		defer restore()

		snapInfo := s.InstallSnap(c, interfaces.ConfinementOptions{}, "", ifacetest.SambaYamlV1, 1)
		profile := filepath.Join(dirs.SnapAppArmorDir, "snap.samba.smbd")
		c.Check(profile, testutil.FileEquals, scenario.expected, Commentf("scenario %d: %#v", i, scenario))
		stat, err := os.Stat(profile)
		c.Assert(err, IsNil)
		c.Check(stat.Mode(), Equals, os.FileMode(0644))
		s.RemoveSnap(c, snapInfo)
	}
}

func (s *backendSuite) TestCombineSnippetsIncludeEtcTunables(c *C) {
	restore := apparmor_sandbox.MockLevel(apparmor_sandbox.Full)
	defer restore()

	restoreTemplate := apparmor.MockTemplate("###INCLUDE_SYSTEM_TUNABLES_HOME_D_WITH_VENDORED_APPARMOR###")
	defer restoreTemplate()

	type includeIfExistsScenario struct {
		features []string
		expected string
	}

	var includeIfExistsScenarios = []includeIfExistsScenario{{
		features: []string{},
		expected: "",
	}, {
		features: []string{"snapd-internal"},
		expected: `#include if exists "/etc/apparmor.d/tunables/home.d"`,
	}}

	for i, scenario := range includeIfExistsScenarios {
		restore = apparmor.MockParserFeatures(func() ([]string, error) { return scenario.features, nil })
		defer restore()

		snapInfo := s.InstallSnap(c, interfaces.ConfinementOptions{}, "", ifacetest.SambaYamlV1, 1)
		profile := filepath.Join(dirs.SnapAppArmorDir, "snap.samba.smbd")
		c.Check(profile, testutil.FileEquals, scenario.expected, Commentf("scenario %d: %#v", i, scenario))
		stat, err := os.Stat(profile)
		c.Assert(err, IsNil)
		c.Check(stat.Mode(), Equals, os.FileMode(0644))
		s.RemoveSnap(c, snapInfo)
	}
}

func (s *backendSuite) TestParallelInstallCombineSnippets(c *C) {
	restore := apparmor_sandbox.MockLevel(apparmor_sandbox.Full)
	defer restore()
	restore = osutil.MockIsHomeUsingNFS(func() (bool, error) { return false, nil })
	defer restore()
	restore = osutil.MockIsRootWritableOverlay(func() (string, error) { return "", nil })
	defer restore()

	// NOTE: replace the real template with a shorter variant
	restoreTemplate := apparmor.MockTemplate("\n" +
		"###VAR###\n" +
		"###PROFILEATTACH### ###FLAGS### {\n" +
		"###SNIPPETS###\n" +
		"}\n")
	defer restoreTemplate()
	restoreClassicTemplate := apparmor.MockClassicTemplate("\n" +
		"#classic\n" +
		"###VAR###\n" +
		"###PROFILEATTACH### ###FLAGS### {\n" +
		"###SNIPPETS###\n" +
		"}\n")
	defer restoreClassicTemplate()
	s.Iface.AppArmorPermanentSlotCallback = func(spec *apparmor.Specification, slot *snap.SlotInfo) error {
		return nil
	}
	expected := `
# This is a snap name without the instance key
@{SNAP_NAME}="samba"
# This is a snap name with instance key
@{SNAP_INSTANCE_NAME}="samba_foo"
@{SNAP_INSTANCE_DESKTOP}="samba+foo"
@{SNAP_COMMAND_NAME}="smbd"
@{SNAP_REVISION}="1"
@{PROFILE_DBUS}="snap_2esamba_5ffoo_2esmbd"
@{INSTALL_DIR}="/{,var/lib/snapd/}snap"
profile "snap.samba_foo.smbd" flags=(attach_disconnected,mediate_deleted) {

}
`
	snapInfo := s.InstallSnap(c, interfaces.ConfinementOptions{}, "samba_foo", ifacetest.SambaYamlV1, 1)
	c.Assert(snapInfo, NotNil)
	profile := filepath.Join(dirs.SnapAppArmorDir, "snap.samba_foo.smbd")
	stat, err := os.Stat(profile)
	c.Assert(err, IsNil)
	c.Check(profile, testutil.FileEquals, expected)
	c.Check(stat.Mode(), Equals, os.FileMode(0644))
	s.RemoveSnap(c, snapInfo)
}

func (s *backendSuite) TestTemplateVarsWithHook(c *C) {
	restore := apparmor_sandbox.MockLevel(apparmor_sandbox.Full)
	defer restore()
	restore = osutil.MockIsHomeUsingNFS(func() (bool, error) { return false, nil })
	defer restore()
	restore = osutil.MockIsRootWritableOverlay(func() (string, error) { return "", nil })
	defer restore()
	// NOTE: replace the real template with a shorter variant
	restoreTemplate := apparmor.MockTemplate("\n" +
		"###VAR###\n" +
		"###PROFILEATTACH### ###FLAGS### {\n" +
		"###SNIPPETS###\n" +
		"}\n")
	defer restoreTemplate()

	expected := `
# This is a snap name without the instance key
@{SNAP_NAME}="foo"
# This is a snap name with instance key
@{SNAP_INSTANCE_NAME}="foo"
@{SNAP_INSTANCE_DESKTOP}="foo"
@{SNAP_COMMAND_NAME}="hook.configure"
@{SNAP_REVISION}="1"
@{PROFILE_DBUS}="snap_2efoo_2ehook_2econfigure"
@{INSTALL_DIR}="/{,var/lib/snapd/}snap"
profile "snap.foo.hook.configure" flags=(attach_disconnected,mediate_deleted) {

}
`
	snapInfo := s.InstallSnap(c, interfaces.ConfinementOptions{}, "", ifacetest.HookYaml, 1)
	c.Assert(snapInfo, NotNil)
	profile := filepath.Join(dirs.SnapAppArmorDir, "snap.foo.hook.configure")
	stat, err := os.Stat(profile)
	c.Assert(err, IsNil)
	c.Check(profile, testutil.FileEquals, expected)
	c.Check(stat.Mode(), Equals, os.FileMode(0644))
	s.RemoveSnap(c, snapInfo)
}

const coreYaml = `name: core
version: 1
type: os
`

const snapdYaml = `name: snapd
version: 1
type: snapd
`

func (s *backendSuite) writeVanillaSnapConfineProfile(c *C, coreOrSnapdInfo *snap.Info) {
	vanillaProfilePath := filepath.Join(coreOrSnapdInfo.MountDir(), "/etc/apparmor.d/usr.lib.snapd.snap-confine.real")
	vanillaProfileText := []byte(`#include <tunables/global>
/usr/lib/snapd/snap-confine (attach_disconnected) {
    #include "/var/lib/snapd/apparmor/snap-confine"

    # We run privileged, so be fanatical about what we include and don't use
    # any abstractions
    /etc/ld.so.cache r,
}
`)
	c.Assert(os.MkdirAll(filepath.Dir(vanillaProfilePath), 0755), IsNil)
	c.Assert(os.WriteFile(vanillaProfilePath, vanillaProfileText, 0644), IsNil)
}

func (s *backendSuite) TestSnapConfineProfile(c *C) {
	// Let's say we're working with the core snap at revision 111.
	coreInfo := snaptest.MockInfo(c, coreYaml, &snap.SideInfo{Revision: snap.R(111)})
	s.writeVanillaSnapConfineProfile(c, coreInfo)
	// We expect to see the same profile, just anchored at a different directory.
	expectedProfileDir := filepath.Join(dirs.GlobalRootDir, "/var/lib/snapd/apparmor/profiles")
	expectedProfileName := "snap-confine.core.111"
	expectedProfileGlob := "snap-confine.core.*"
	expectedProfileText := fmt.Sprintf(`#include <tunables/global>
%s/usr/lib/snapd/snap-confine (attach_disconnected) {
    #include "%s/var/lib/snapd/apparmor/snap-confine"

    # We run privileged, so be fanatical about what we include and don't use
    # any abstractions
    /etc/ld.so.cache r,
}
`, coreInfo.MountDir(), dirs.GlobalRootDir)

	c.Assert(expectedProfileName, testutil.Contains, coreInfo.Revision.String())

	// Compute the profile and see if it matches.
	dir, glob, content, err := apparmor.SnapConfineFromSnapProfile(coreInfo)
	c.Assert(err, IsNil)
	c.Assert(dir, Equals, expectedProfileDir)
	c.Assert(glob, Equals, expectedProfileGlob)
	c.Assert(content, DeepEquals, map[string]osutil.FileState{
		expectedProfileName: &osutil.MemoryFileState{
			Content: []byte(expectedProfileText),
			Mode:    0644,
		},
	})
}

func (s *backendSuite) TestSnapConfineProfileFromSnapdSnap(c *C) {
	restore := release.MockOnClassic(false)
	defer restore()
	dirs.SetRootDir(s.RootDir)

	snapdInfo := snaptest.MockInfo(c, snapdYaml, &snap.SideInfo{Revision: snap.R(222)})
	s.writeVanillaSnapConfineProfile(c, snapdInfo)

	// We expect to see the same profile, just anchored at a different directory.
	expectedProfileDir := filepath.Join(dirs.GlobalRootDir, "/var/lib/snapd/apparmor/profiles")
	expectedProfileName := "snap-confine.snapd.222"
	expectedProfileGlob := "snap-confine.snapd.222"
	expectedProfileText := fmt.Sprintf(`#include <tunables/global>
%s/usr/lib/snapd/snap-confine (attach_disconnected) {
    #include "%s/var/lib/snapd/apparmor/snap-confine"

    # We run privileged, so be fanatical about what we include and don't use
    # any abstractions
    /etc/ld.so.cache r,
}
`, snapdInfo.MountDir(), dirs.GlobalRootDir)

	c.Assert(expectedProfileName, testutil.Contains, snapdInfo.Revision.String())

	// Compute the profile and see if it matches.
	dir, glob, content, err := apparmor.SnapConfineFromSnapProfile(snapdInfo)
	c.Assert(err, IsNil)
	c.Assert(dir, Equals, expectedProfileDir)
	c.Assert(glob, Equals, expectedProfileGlob)
	c.Assert(content, DeepEquals, map[string]osutil.FileState{
		expectedProfileName: &osutil.MemoryFileState{
			Content: []byte(expectedProfileText),
			Mode:    0644,
		},
	})
}

func (s *backendSuite) TestSnapConfineProfileUsesSandboxSnapConfineDir(c *C) {
	snapdInfo := snaptest.MockInfo(c, snapdYaml, &snap.SideInfo{Revision: snap.R(222)})
	s.writeVanillaSnapConfineProfile(c, snapdInfo)
	expectedProfileName := "snap-confine.snapd.222"

	// Compute the profile and see that it replaces
	// "/var/lib/snapd/apparmor/snap-confine" with the
	// apparmor_sandbox.SnapConfineAppArmorDir dir
	apparmor_sandbox.SnapConfineAppArmorDir = "/apparmor/sandbox/dir"
	_, _, content, err := apparmor.SnapConfineFromSnapProfile(snapdInfo)
	c.Assert(err, IsNil)
	contentStr := string(content[expectedProfileName].(*osutil.MemoryFileState).Content)
	c.Check(contentStr, testutil.Contains, `   #include "/apparmor/sandbox/dir"`)
}

func (s *backendSuite) TestSnapConfineFromSnapProfileCreatesAllDirs(c *C) {
	c.Assert(osutil.IsDirectory(dirs.SnapAppArmorDir), Equals, false)
	coreInfo := snaptest.MockInfo(c, coreYaml, &snap.SideInfo{Revision: snap.R(111)})

	s.writeVanillaSnapConfineProfile(c, coreInfo)

	aa := &apparmor.Backend{}
	err := aa.SetupSnapConfineReexec(coreInfo)
	c.Assert(err, IsNil)
	c.Assert(osutil.IsDirectory(dirs.SnapAppArmorDir), Equals, true)
}

func (s *backendSuite) TestSnapConfineProfileIncludesEtcTunables(c *C) {
	// Mock vendored apparmor
	r := apparmor.MockParserFeatures(func() ([]string, error) { return []string{"snapd-internal"}, nil })
	defer r()

	// Let's say we're working with the core snap at revision 111.
	coreInfo := snaptest.MockInfo(c, coreYaml, &snap.SideInfo{Revision: snap.R(111)})
	s.writeVanillaSnapConfineProfile(c, coreInfo)

	// Expect #include if exists "/etc/apparmor.d/tunables/home.d/" to be added
	// right below #include <tunables/global>
	expectedProfileName := "snap-confine.core.111"
	expectedProfileText := fmt.Sprintf(`#include <tunables/global>
#include if exists "/etc/apparmor.d/tunables/home.d/"
%s/usr/lib/snapd/snap-confine (attach_disconnected) {
    #include "%s/var/lib/snapd/apparmor/snap-confine"

    # We run privileged, so be fanatical about what we include and don't use
    # any abstractions
    /etc/ld.so.cache r,
}
`, coreInfo.MountDir(), dirs.GlobalRootDir)

	// Compute the profile and see if it matches.
	_, _, content, err := apparmor.SnapConfineFromSnapProfile(coreInfo)
	c.Assert(err, IsNil)
	c.Assert(content, DeepEquals, map[string]osutil.FileState{
		expectedProfileName: &osutil.MemoryFileState{
			Content: []byte(expectedProfileText),
			Mode:    0644,
		},
	})
}

func (s *backendSuite) TestSetupHostSnapConfineApparmorForReexecCleans(c *C) {
	restorer := release.MockOnClassic(true)
	defer restorer()
	restorer = apparmor_sandbox.MockLevel(apparmor_sandbox.Full)
	defer restorer()

	coreInfo := snaptest.MockInfo(c, coreYaml, &snap.SideInfo{Revision: snap.R(111)})
	s.writeVanillaSnapConfineProfile(c, coreInfo)

	canaryName := "snap-confine.core.2718"
	canary := filepath.Join(dirs.SnapAppArmorDir, canaryName)
	err := os.MkdirAll(filepath.Dir(canary), 0755)
	c.Assert(err, IsNil)
	err = os.WriteFile(canary, nil, 0644)
	c.Assert(err, IsNil)

	// install the new core snap on classic triggers cleanup
	s.InstallSnap(c, interfaces.ConfinementOptions{}, "", coreYaml, 111)

	c.Check(canary, testutil.FileAbsent)
}

func (s *backendSuite) TestSetupHostSnapConfineApparmorForReexecWritesNew(c *C) {
	restorer := release.MockOnClassic(true)
	defer restorer()
	restorer = apparmor_sandbox.MockLevel(apparmor_sandbox.Full)
	defer restorer()

	coreInfo := snaptest.MockInfo(c, coreYaml, &snap.SideInfo{Revision: snap.R(111)})
	s.writeVanillaSnapConfineProfile(c, coreInfo)

	// Install the new core snap on classic triggers a new snap-confine
	// for this snap-confine on core
	s.InstallSnap(c, interfaces.ConfinementOptions{}, "", coreYaml, 111)

	newAA, err := filepath.Glob(filepath.Join(dirs.SnapAppArmorDir, "*"))
	c.Assert(err, IsNil)
	c.Assert(newAA, HasLen, 1)
	c.Check(newAA[0], Matches, `.*/var/lib/snapd/apparmor/profiles/snap-confine.core.111`)

	// This is the key, rewriting "/usr/lib/snapd/snap-confine
	c.Check(newAA[0], testutil.FileContains, "/snap/core/111/usr/lib/snapd/snap-confine (attach_disconnected) {")
	// No other changes other than that to the input
	c.Check(newAA[0], testutil.FileEquals, fmt.Sprintf(`#include <tunables/global>
%s/core/111/usr/lib/snapd/snap-confine (attach_disconnected) {
    #include "%s/var/lib/snapd/apparmor/snap-confine"

    # We run privileged, so be fanatical about what we include and don't use
    # any abstractions
    /etc/ld.so.cache r,
}
`, dirs.SnapMountDir, dirs.GlobalRootDir))

	c.Check(s.loadProfilesCalls, DeepEquals, []loadProfilesParams{
		{[]string{newAA[0]}, fmt.Sprintf("%s/var/cache/apparmor", s.RootDir), 0},
	})

	// snap-confine directory was created
	_, err = os.Stat(apparmor_sandbox.SnapConfineAppArmorDir)
	c.Check(err, IsNil)
}

func (s *backendSuite) TestSnapConfineProfileDiscardedLateSnapd(c *C) {
	restorer := release.MockOnClassic(false)
	defer restorer()
	restorer = apparmor_sandbox.MockLevel(apparmor_sandbox.Full)
	defer restorer()
	// snapd snap at revision 222.
	snapdInfo := snaptest.MockInfo(c, snapdYaml, &snap.SideInfo{Revision: snap.R(222)})
	appSet := interfaces.NewSnapAppSet(snapdInfo)
	s.writeVanillaSnapConfineProfile(c, snapdInfo)
	err := s.Backend.Setup(appSet, interfaces.ConfinementOptions{}, s.Repo, s.perf)
	c.Assert(err, IsNil)
	// precondition
	c.Assert(filepath.Join(dirs.SnapAppArmorDir, "snap-confine.snapd.222"), testutil.FilePresent)
	// place a canary
	c.Assert(os.WriteFile(filepath.Join(dirs.SnapAppArmorDir, "snap-confine.snapd.111"), nil, 0644), IsNil)

	// backed implements the right interface
	late, ok := s.Backend.(interfaces.SecurityBackendDiscardingLate)
	c.Assert(ok, Equals, true)
	err = late.RemoveLate(snapdInfo.InstanceName(), snapdInfo.Revision, snapdInfo.Type())
	c.Assert(err, IsNil)
	c.Check(filepath.Join(dirs.SnapAppArmorDir, "snap-confine.snapd.222"), testutil.FileAbsent)
	// but the canary is still present
	c.Assert(filepath.Join(dirs.SnapAppArmorDir, "snap-confine.snapd.111"), testutil.FilePresent)
}

func (s *backendSuite) TestCoreOnCoreCleansApparmorCache(c *C) {
	coreInfo := snaptest.MockInfo(c, coreYaml, &snap.SideInfo{Revision: snap.R(111)})
	s.writeVanillaSnapConfineProfile(c, coreInfo)
	s.testCoreOrSnapdOnCoreCleansApparmorCache(c, coreYaml)
}

func (s *backendSuite) TestSnapdOnCoreCleansApparmorCache(c *C) {
	snapdInfo := snaptest.MockInfo(c, snapdYaml, &snap.SideInfo{Revision: snap.R(111)})
	s.writeVanillaSnapConfineProfile(c, snapdInfo)
	s.testCoreOrSnapdOnCoreCleansApparmorCache(c, snapdYaml)
}

func (s *backendSuite) testCoreOrSnapdOnCoreCleansApparmorCache(c *C, coreOrSnapdYaml string) {
	restorer := release.MockOnClassic(false)
	defer restorer()

	err := os.MkdirAll(apparmor_sandbox.SystemCacheDir, 0755)
	c.Assert(err, IsNil)
	// the canary file in the cache will be removed
	canaryPath := filepath.Join(apparmor_sandbox.SystemCacheDir, "meep")
	err = os.WriteFile(canaryPath, nil, 0644)
	c.Assert(err, IsNil)
	// and the snap-confine profiles are removed
	scCanaryPath := filepath.Join(apparmor_sandbox.SystemCacheDir, "usr.lib.snapd.snap-confine.real")
	err = os.WriteFile(scCanaryPath, nil, 0644)
	c.Assert(err, IsNil)
	scCanaryPath = filepath.Join(apparmor_sandbox.SystemCacheDir, "usr.lib.snapd.snap-confine")
	err = os.WriteFile(scCanaryPath, nil, 0644)
	c.Assert(err, IsNil)
	scCanaryPath = filepath.Join(apparmor_sandbox.SystemCacheDir, "snap-confine.core.6405")
	err = os.WriteFile(scCanaryPath, nil, 0644)
	c.Assert(err, IsNil)
	scCanaryPath = filepath.Join(apparmor_sandbox.SystemCacheDir, "snap-confine.snapd.6405")
	err = os.WriteFile(scCanaryPath, nil, 0644)
	c.Assert(err, IsNil)
	scCanaryPath = filepath.Join(apparmor_sandbox.SystemCacheDir, "snap.core.4938.usr.lib.snapd.snap-confine")
	err = os.WriteFile(scCanaryPath, nil, 0644)
	c.Assert(err, IsNil)
	scCanaryPath = filepath.Join(apparmor_sandbox.SystemCacheDir, "var.lib.snapd.snap.core.1234.usr.lib.snapd.snap-confine")
	err = os.WriteFile(scCanaryPath, nil, 0644)
	c.Assert(err, IsNil)
	// but non-regular entries in the cache dir are kept
	dirsAreKept := filepath.Join(apparmor_sandbox.SystemCacheDir, "dir")
	err = os.MkdirAll(dirsAreKept, 0755)
	c.Assert(err, IsNil)
	symlinksAreKept := filepath.Join(apparmor_sandbox.SystemCacheDir, "symlink")
	err = os.Symlink("some-sylink-target", symlinksAreKept)
	c.Assert(err, IsNil)
	// and the snap profiles are kept
	snapCanaryKept := filepath.Join(apparmor_sandbox.SystemCacheDir, "snap.canary.meep")
	err = os.WriteFile(snapCanaryKept, nil, 0644)
	c.Assert(err, IsNil)
	sunCanaryKept := filepath.Join(apparmor_sandbox.SystemCacheDir, "snap-update-ns.canary")
	err = os.WriteFile(sunCanaryKept, nil, 0644)
	c.Assert(err, IsNil)
	// and the .features file is kept
	dotKept := filepath.Join(apparmor_sandbox.SystemCacheDir, ".features")
	err = os.WriteFile(dotKept, nil, 0644)
	c.Assert(err, IsNil)

	// install the new core snap on classic triggers a new snap-confine
	// for this snap-confine on core
	s.InstallSnap(c, interfaces.ConfinementOptions{}, "", coreOrSnapdYaml, 111)

	l, err := filepath.Glob(filepath.Join(apparmor_sandbox.SystemCacheDir, "*"))
	c.Assert(err, IsNil)
	// canary is gone, extra stuff is kept
	c.Check(l, DeepEquals, []string{dotKept, dirsAreKept, sunCanaryKept, snapCanaryKept, symlinksAreKept})
}

// Ensure that both names of the snap-confine apparmor profile are supported.
func (s *backendSuite) TestSetupSnapConfineGeneratedPolicyWithNFS1(c *C) {
	s.testSetupSnapConfineGeneratedPolicyWithNFS(c, "usr.lib.snapd.snap-confine")
}

func (s *backendSuite) TestSetupSnapConfineGeneratedPolicyWithNFS2(c *C) {
	s.testSetupSnapConfineGeneratedPolicyWithNFS(c, "usr.lib.snapd.snap-confine.real")
}

func (s *backendSuite) TestSetupSnapConfineGeneratedPolicyWithNFSNoProfileFiles(c *C) {
	// Make it appear as if NFS workaround was needed.
	restore := osutil.MockIsHomeUsingNFS(func() (bool, error) { return true, nil })
	defer restore()
	// Make it appear as if overlay was not used.
	restore = osutil.MockIsRootWritableOverlay(func() (string, error) { return "", nil })
	defer restore()

	// Intercept interaction with apparmor_parser
	cmd := testutil.MockCommand(c, "apparmor_parser", "")
	defer cmd.Restore()
	// Set up apparmor profiles directory, but no profile for snap-confine
	c.Assert(os.MkdirAll(apparmor_sandbox.ConfDir, 0755), IsNil)

	// The apparmor backend should not fail if the apparmor profile of
	// snap-confine is not present
	err := (&apparmor.Backend{}).Initialize(ifacetest.DefaultInitializeOpts)
	c.Assert(err, IsNil)
	// Since there is no profile file, no call to apparmor were made
	c.Assert(cmd.Calls(), HasLen, 0)
}

// snap-confine policy when NFS is used and snapd has not re-executed.
func (s *backendSuite) testSetupSnapConfineGeneratedPolicyWithNFS(c *C, profileFname string) {
	// Make it appear as if NFS workaround was needed.
	restore := osutil.MockIsHomeUsingNFS(func() (bool, error) { return true, nil })
	defer restore()

	// Make it appear as if overlay was not used.
	restore = osutil.MockIsRootWritableOverlay(func() (string, error) { return "", nil })
	defer restore()

	// Intercept the /proc/self/exe symlink and point it to the distribution
	// executable (the path doesn't matter as long as it is not from the
	// mounted core snap). This indicates that snapd is not re-executing
	// and that we should reload snap-confine profile.
	fakeExe := filepath.Join(s.RootDir, "fake-proc-self-exe")
	err := os.Symlink("/usr/lib/snapd/snapd", fakeExe)
	c.Assert(err, IsNil)
	restore = apparmor.MockProcSelfExe(fakeExe)
	defer restore()

	profilePath := filepath.Join(apparmor_sandbox.ConfDir, profileFname)

	// Create the directory where system apparmor profiles are stored and write
	// the system apparmor profile of snap-confine.
	c.Assert(os.MkdirAll(apparmor_sandbox.ConfDir, 0755), IsNil)
	c.Assert(os.WriteFile(profilePath, []byte(""), 0644), IsNil)

	// Setup generated policy for snap-confine.
	err = (&apparmor.Backend{}).Initialize(ifacetest.DefaultInitializeOpts)
	c.Assert(err, IsNil)

	// Because NFS is being used, we have the extra policy file.
	files, err := ioutil.ReadDir(apparmor_sandbox.SnapConfineAppArmorDir)
	c.Assert(err, IsNil)
	c.Assert(files, HasLen, 1)
	c.Assert(files[0].Name(), Equals, "nfs-support")
	c.Assert(files[0].Mode(), Equals, os.FileMode(0644))
	c.Assert(files[0].IsDir(), Equals, false)

	// The policy allows network access.
	fn := filepath.Join(apparmor_sandbox.SnapConfineAppArmorDir, files[0].Name())
	c.Assert(fn, testutil.FileContains, "network inet,")
	c.Assert(fn, testutil.FileContains, "network inet6,")

	// The system apparmor profile of snap-confine was reloaded.
	c.Assert(s.loadProfilesCalls, DeepEquals, []loadProfilesParams{{
		[]string{profilePath},
		apparmor_sandbox.SystemCacheDir,
		apparmor_sandbox.SkipReadCache,
	}})
}

// snap-confine policy when NFS is used and snapd has re-executed.
func (s *backendSuite) TestSetupSnapConfineGeneratedPolicyWithNFSAndReExec(c *C) {
	// Make it appear as if NFS workaround was needed.
	restore := osutil.MockIsHomeUsingNFS(func() (bool, error) { return true, nil })
	defer restore()

	// Make it appear as if overlay was not used.
	restore = osutil.MockIsRootWritableOverlay(func() (string, error) { return "", nil })
	defer restore()

	// Intercept interaction with apparmor_parser
	cmd := testutil.MockCommand(c, "apparmor_parser", "")
	defer cmd.Restore()

	// Intercept the /proc/self/exe symlink and point it to the snapd from the
	// mounted core snap. This indicates that snapd has re-executed and
	// should not reload snap-confine policy.
	fakeExe := filepath.Join(s.RootDir, "fake-proc-self-exe")
	err := os.Symlink(filepath.Join(dirs.SnapMountDir, "/core/1234/usr/lib/snapd/snapd"), fakeExe)
	c.Assert(err, IsNil)
	restore = apparmor.MockProcSelfExe(fakeExe)
	defer restore()

	// Setup generated policy for snap-confine.
	err = (&apparmor.Backend{}).Initialize(ifacetest.DefaultInitializeOpts)
	c.Assert(err, IsNil)

	// Because NFS is being used, we have the extra policy file.
	files, err := ioutil.ReadDir(apparmor_sandbox.SnapConfineAppArmorDir)
	c.Assert(err, IsNil)
	c.Assert(files, HasLen, 1)
	c.Assert(files[0].Name(), Equals, "nfs-support")
	c.Assert(files[0].Mode(), Equals, os.FileMode(0644))
	c.Assert(files[0].IsDir(), Equals, false)

	// The policy allows network access.
	fn := filepath.Join(apparmor_sandbox.SnapConfineAppArmorDir, files[0].Name())
	c.Assert(fn, testutil.FileContains, "network inet,")
	c.Assert(fn, testutil.FileContains, "network inet6,")

	// The distribution policy was not reloaded because snap-confine executes
	// from core snap. This is handled separately by per-profile Setup.
	c.Assert(cmd.Calls(), HasLen, 0)
}

// Test behavior when os.Readlink "/proc/self/exe" fails.
func (s *backendSuite) TestSetupSnapConfineGeneratedPolicyError1(c *C) {
	// Make it appear as if NFS workaround was needed.
	restore := osutil.MockIsHomeUsingNFS(func() (bool, error) { return true, nil })
	defer restore()

	// Make it appear as if overlay was not used.
	restore = osutil.MockIsRootWritableOverlay(func() (string, error) { return "", nil })
	defer restore()

	// Intercept interaction with apparmor_parser
	cmd := testutil.MockCommand(c, "apparmor_parser", "")
	defer cmd.Restore()

	// Intercept the /proc/self/exe symlink and make it point to something that
	// doesn't exist (break it).
	fakeExe := filepath.Join(s.RootDir, "corrupt-proc-self-exe")
	restore = apparmor.MockProcSelfExe(fakeExe)
	defer restore()

	// Setup generated policy for snap-confine.
	err := (&apparmor.Backend{}).Initialize(ifacetest.DefaultInitializeOpts)
	c.Assert(err, ErrorMatches, "cannot read .*corrupt-proc-self-exe: .*")

	// We didn't create the policy file.
	files, err := ioutil.ReadDir(apparmor_sandbox.SnapConfineAppArmorDir)
	c.Assert(err, IsNil)
	c.Assert(files, HasLen, 0)

	// We didn't reload the policy though.
	c.Assert(cmd.Calls(), HasLen, 0)
}

// Test behavior when exec.Command "apparmor_parser" fails
func (s *backendSuite) TestSetupSnapConfineGeneratedPolicyError2(c *C) {
	// Make it appear as if NFS workaround was needed.
	restore := osutil.MockIsHomeUsingNFS(func() (bool, error) { return true, nil })
	defer restore()

	// Make it appear as if overlay was not used.
	restore = osutil.MockIsRootWritableOverlay(func() (string, error) { return "", nil })
	defer restore()

	// Intercept interaction with apparmor_parser and make it fail.
	s.loadProfilesReturn = errors.New("bad luck")

	// Intercept the /proc/self/exe symlink.
	fakeExe := filepath.Join(s.RootDir, "fake-proc-self-exe")
	err := os.Symlink("/usr/lib/snapd/snapd", fakeExe)
	c.Assert(err, IsNil)
	restore = apparmor.MockProcSelfExe(fakeExe)
	defer restore()

	// Create the directory where system apparmor profiles are stored and Write
	// the system apparmor profile of snap-confine.
	c.Assert(os.MkdirAll(apparmor_sandbox.ConfDir, 0755), IsNil)
	c.Assert(os.WriteFile(filepath.Join(apparmor_sandbox.ConfDir, "usr.lib.snapd.snap-confine"), []byte(""), 0644), IsNil)

	// Setup generated policy for snap-confine.
	err = (&apparmor.Backend{}).Initialize(ifacetest.DefaultInitializeOpts)
	c.Assert(err, ErrorMatches, "cannot reload snap-confine apparmor profile: bad luck")

	// While created the policy file initially we also removed it so that
	// no side-effects remain.
	files, err := ioutil.ReadDir(apparmor_sandbox.SnapConfineAppArmorDir)
	c.Assert(err, IsNil)
	c.Assert(files, HasLen, 0)

	// We tried to reload the policy.
	c.Assert(s.loadProfilesCalls, HasLen, 1)
}

// Ensure that both names of the snap-confine apparmor profile are supported.
func (s *backendSuite) TestSetupSnapConfineGeneratedPolicyWithOverlay1(c *C) {
	s.testSetupSnapConfineGeneratedPolicyWithOverlay(c, "usr.lib.snapd.snap-confine")
}

func (s *backendSuite) TestSetupSnapConfineGeneratedPolicyWithOverlay2(c *C) {
	s.testSetupSnapConfineGeneratedPolicyWithOverlay(c, "usr.lib.snapd.snap-confine.real")
}

func (s *backendSuite) TestSetupSnapConfineGeneratedPolicyWithOverlayNoProfileFiles(c *C) {
	// Make it appear as if overlay workaround was needed.
	restore := osutil.MockIsRootWritableOverlay(func() (string, error) { return "/upper", nil })
	defer restore()
	// No NFS workaround
	restore = osutil.MockIsHomeUsingNFS(func() (bool, error) { return false, nil })
	defer restore()

	// Intercept interaction with apparmor_parser
	cmd := testutil.MockCommand(c, "apparmor_parser", "")
	defer cmd.Restore()
	// Set up apparmor profiles directory, but no profile for snap-confine
	c.Assert(os.MkdirAll(apparmor_sandbox.ConfDir, 0755), IsNil)

	// The apparmor backend should not fail if the apparmor profile of
	// snap-confine is not present
	err := (&apparmor.Backend{}).Initialize(ifacetest.DefaultInitializeOpts)
	c.Assert(err, IsNil)
	// Since there is no profile file, no call to apparmor were made
	c.Assert(cmd.Calls(), HasLen, 0)
}

// snap-confine policy when overlay is used and snapd has not re-executed.
func (s *backendSuite) testSetupSnapConfineGeneratedPolicyWithOverlay(c *C, profileFname string) {
	// Make it appear as if overlay workaround was needed.
	restore := osutil.MockIsRootWritableOverlay(func() (string, error) { return "/upper", nil })
	defer restore()
	restore = osutil.MockIsHomeUsingNFS(func() (bool, error) { return false, nil })
	defer restore()

	// Intercept the /proc/self/exe symlink and point it to the distribution
	// executable (the path doesn't matter as long as it is not from the
	// mounted core snap). This indicates that snapd is not re-executing
	// and that we should reload snap-confine profile.
	fakeExe := filepath.Join(s.RootDir, "fake-proc-self-exe")
	err := os.Symlink("/usr/lib/snapd/snapd", fakeExe)
	c.Assert(err, IsNil)
	restore = apparmor.MockProcSelfExe(fakeExe)
	defer restore()

	profilePath := filepath.Join(apparmor_sandbox.ConfDir, profileFname)

	// Create the directory where system apparmor profiles are stored and write
	// the system apparmor profile of snap-confine.
	c.Assert(os.MkdirAll(apparmor_sandbox.ConfDir, 0755), IsNil)
	c.Assert(os.WriteFile(profilePath, []byte(""), 0644), IsNil)

	// Setup generated policy for snap-confine.
	err = (&apparmor.Backend{}).Initialize(ifacetest.DefaultInitializeOpts)
	c.Assert(err, IsNil)

	// Because overlay is being used, we have the extra policy file.
	files, err := ioutil.ReadDir(apparmor_sandbox.SnapConfineAppArmorDir)
	c.Assert(err, IsNil)
	c.Assert(files, HasLen, 1)
	c.Assert(files[0].Name(), Equals, "overlay-root")
	c.Assert(files[0].Mode(), Equals, os.FileMode(0644))
	c.Assert(files[0].IsDir(), Equals, false)

	// The policy allows upperdir access.
	data, err := ioutil.ReadFile(filepath.Join(apparmor_sandbox.SnapConfineAppArmorDir, files[0].Name()))
	c.Assert(err, IsNil)
	c.Assert(string(data), testutil.Contains, "\"/upper/{,**/}\" r,")

	// The system apparmor profile of snap-confine was reloaded.
	c.Assert(s.loadProfilesCalls, DeepEquals, []loadProfilesParams{{
		[]string{profilePath},
		apparmor_sandbox.SystemCacheDir,
		apparmor_sandbox.SkipReadCache,
	}})
}

// snap-confine policy when overlay is used and snapd has re-executed.
func (s *backendSuite) TestSetupSnapConfineGeneratedPolicyWithOverlayAndReExec(c *C) {
	// Make it appear as if overlay workaround was needed.
	restore := osutil.MockIsRootWritableOverlay(func() (string, error) { return "/upper", nil })
	defer restore()

	restore = osutil.MockIsHomeUsingNFS(func() (bool, error) { return false, nil })
	defer restore()

	// Intercept interaction with apparmor_parser
	cmd := testutil.MockCommand(c, "apparmor_parser", "")
	defer cmd.Restore()

	// Intercept the /proc/self/exe symlink and point it to the snapd from the
	// mounted core snap. This indicates that snapd has re-executed and
	// should not reload snap-confine policy.
	fakeExe := filepath.Join(s.RootDir, "fake-proc-self-exe")
	err := os.Symlink(filepath.Join(dirs.SnapMountDir, "/core/1234/usr/lib/snapd/snapd"), fakeExe)
	c.Assert(err, IsNil)
	restore = apparmor.MockProcSelfExe(fakeExe)
	defer restore()

	// Setup generated policy for snap-confine.
	err = (&apparmor.Backend{}).Initialize(ifacetest.DefaultInitializeOpts)
	c.Assert(err, IsNil)

	// Because overlay is being used, we have the extra policy file.
	files, err := ioutil.ReadDir(apparmor_sandbox.SnapConfineAppArmorDir)
	c.Assert(err, IsNil)
	c.Assert(files, HasLen, 1)
	c.Assert(files[0].Name(), Equals, "overlay-root")
	c.Assert(files[0].Mode(), Equals, os.FileMode(0644))
	c.Assert(files[0].IsDir(), Equals, false)

	// The policy allows upperdir access
	data, err := ioutil.ReadFile(filepath.Join(apparmor_sandbox.SnapConfineAppArmorDir, files[0].Name()))
	c.Assert(err, IsNil)
	c.Assert(string(data), testutil.Contains, "\"/upper/{,**/}\" r,")

	// The distribution policy was not reloaded because snap-confine executes
	// from core snap. This is handled separately by per-profile Setup.
	c.Assert(cmd.Calls(), HasLen, 0)
}

func (s *backendSuite) testSetupSnapConfineGeneratedPolicyWithBPFCapability(c *C, reexec bool) {
	restore := osutil.MockIsRootWritableOverlay(func() (string, error) { return "", nil })
	defer restore()
	restore = osutil.MockIsHomeUsingNFS(func() (bool, error) { return false, nil })
	defer restore()
	// Pretend apparmor_parser supports bpf capability
	apparmor_sandbox.MockFeatures(nil, nil, []string{"cap-bpf"}, nil)

	// Hijack interaction with apparmor_parser
	cmd := testutil.MockCommand(c, "apparmor_parser", "")
	defer cmd.Restore()

	fakeExe := filepath.Join(s.RootDir, "fake-proc-self-exe")
	restore = apparmor.MockProcSelfExe(fakeExe)
	defer restore()
	if reexec {
		// Pretend snapd is reexecuted from the core snap
		err := os.Symlink(filepath.Join(dirs.SnapMountDir, "/core/1234/usr/lib/snapd/snapd"), fakeExe)
		c.Assert(err, IsNil)
	} else {
		// Pretend snapd is executing from the native package
		err := os.Symlink("/usr/lib/snapd/snapd", fakeExe)
		c.Assert(err, IsNil)
	}

	profilePath := filepath.Join(apparmor_sandbox.ConfDir, "usr.lib.snapd.snap-confine")
	// Create the directory where system apparmor profiles are stored and write
	// the system apparmor profile of snap-confine.
	c.Assert(os.MkdirAll(apparmor_sandbox.ConfDir, 0755), IsNil)
	c.Assert(os.WriteFile(profilePath, []byte(""), 0644), IsNil)

	// Setup generated policy for snap-confine.
	err := (&apparmor.Backend{}).Initialize(ifacetest.DefaultInitializeOpts)
	c.Assert(err, IsNil)

	// Capability bpf is supported by the parser, so an extra policy file
	// for snap-confine is present
	files, err := ioutil.ReadDir(apparmor_sandbox.SnapConfineAppArmorDir)
	c.Assert(err, IsNil)
	c.Assert(files, HasLen, 1)
	c.Assert(files[0].Name(), Equals, "cap-bpf")
	c.Assert(files[0].Mode(), Equals, os.FileMode(0644))
	c.Assert(files[0].IsDir(), Equals, false)

	c.Assert(filepath.Join(apparmor_sandbox.SnapConfineAppArmorDir, files[0].Name()),
		testutil.FileContains, "capability bpf,")

	if reexec {
		// The distribution policy was not reloaded because snap-confine executes
		// from core snap. This is handled separately by per-profile Setup.
		c.Assert(s.loadProfilesCalls, HasLen, 0)
	} else {
		c.Assert(s.loadProfilesCalls, DeepEquals, []loadProfilesParams{{
			[]string{profilePath},
			apparmor_sandbox.SystemCacheDir,
			apparmor_sandbox.SkipReadCache,
		}})
	}
}

// snap-confine policy when apparmor_parser supports BPF capability and snapd reexec
func (s *backendSuite) TestSetupSnapConfineGeneratedPolicyWithBPFCapabilityReexec(c *C) {
	const reexecd = true
	s.testSetupSnapConfineGeneratedPolicyWithBPFCapability(c, reexecd)
}

// snap-confine policy when apparmor_parser supports BPF capability but no reexec
func (s *backendSuite) TestSetupSnapConfineGeneratedPolicyWithBPFCapabilityNoReexec(c *C) {
	const reexecd = false
	s.testSetupSnapConfineGeneratedPolicyWithBPFCapability(c, reexecd)
}

func (s *backendSuite) TestSetupSnapConfineGeneratedPolicyWithBPFProbeError(c *C) {
	log, restore := logger.MockLogger()
	defer restore()
	restore = osutil.MockIsRootWritableOverlay(func() (string, error) { return "", nil })
	defer restore()
	restore = osutil.MockIsHomeUsingNFS(func() (bool, error) { return false, nil })
	defer restore()
	// Probing for apparmor_parser features failed
	apparmor_sandbox.MockFeatures(nil, nil, nil, fmt.Errorf("mock probe error"))

	// Hijack interaction with apparmor_parser
	cmd := testutil.MockCommand(c, "apparmor_parser", "")
	defer cmd.Restore()

	fakeExe := filepath.Join(s.RootDir, "fake-proc-self-exe")
	restore = apparmor.MockProcSelfExe(fakeExe)
	defer restore()
	// Pretend snapd is executing from the native package
	err := os.Symlink("/usr/lib/snapd/snapd", fakeExe)
	c.Assert(err, IsNil)

	profilePath := filepath.Join(apparmor_sandbox.ConfDir, "usr.lib.snapd.snap-confine")
	// Create the directory where system apparmor profiles are stored and write
	// the system apparmor profile of snap-confine.
	c.Assert(os.MkdirAll(apparmor_sandbox.ConfDir, 0755), IsNil)
	c.Assert(os.WriteFile(profilePath, []byte(""), 0644), IsNil)

	// Setup generated policy for snap-confine.
	err = (&apparmor.Backend{}).Initialize(ifacetest.DefaultInitializeOpts)
	c.Assert(err, IsNil)

	// Probing apparmor_parser capabilities failed, so nothing gets written
	// to the snap-confine policy directory
	files, err := ioutil.ReadDir(apparmor_sandbox.SnapConfineAppArmorDir)
	c.Assert(err, IsNil)
	c.Assert(files, HasLen, 0)

	// No calls to apparmor_parser
	c.Assert(cmd.Calls(), HasLen, 0)

	// But an error was logged
	c.Assert(log.String(), testutil.Contains, "cannot determine apparmor_parser features: mock probe error")
}

type nfsAndOverlaySnippetsScenario struct {
	opts           interfaces.ConfinementOptions
	overlaySnippet string
	nfsSnippet     string
}

var nfsAndOverlaySnippetsScenarios = []nfsAndOverlaySnippetsScenario{{
	// By default apparmor is enforcing mode.
	opts:           interfaces.ConfinementOptions{},
	overlaySnippet: `"/upper/{,**/}" r,`,
	nfsSnippet:     "network inet,\n  network inet6,",
}, {
	// DevMode switches apparmor to non-enforcing (complain) mode.
	opts:           interfaces.ConfinementOptions{DevMode: true},
	overlaySnippet: `"/upper/{,**/}" r,`,
	nfsSnippet:     "network inet,\n  network inet6,",
}, {
	// JailMode switches apparmor to enforcing mode even in the presence of DevMode.
	opts:           interfaces.ConfinementOptions{DevMode: true, JailMode: true},
	overlaySnippet: `"/upper/{,**/}" r,`,
	nfsSnippet:     "network inet,\n  network inet6,",
}, {
	// Classic confinement (without jailmode) uses apparmor in complain mode by default and ignores all snippets.
	opts:           interfaces.ConfinementOptions{Classic: true},
	overlaySnippet: "",
	nfsSnippet:     "",
}, {
	// Classic confinement in JailMode uses enforcing apparmor.
	opts: interfaces.ConfinementOptions{Classic: true, JailMode: true},
	// FIXME: logic in backend.addContent is wrong for this case
	//overlaySnippet: `"/upper/{,**/}" r,`,
	//nfsSnippet: "network inet,\n  network inet6,",
	overlaySnippet: "",
	nfsSnippet:     "",
}}

func (s *backendSuite) TestNFSAndOverlaySnippets(c *C) {
	restore := apparmor_sandbox.MockLevel(apparmor_sandbox.Full)
	defer restore()
	restore = osutil.MockIsHomeUsingNFS(func() (bool, error) { return true, nil })
	defer restore()
	restore = osutil.MockIsRootWritableOverlay(func() (string, error) { return "/upper", nil })
	defer restore()
	s.Iface.AppArmorPermanentSlotCallback = func(spec *apparmor.Specification, slot *snap.SlotInfo) error {
		return nil
	}

	for _, scenario := range nfsAndOverlaySnippetsScenarios {
		snapInfo := s.InstallSnap(c, scenario.opts, "", ifacetest.SambaYamlV1, 1)
		profile := filepath.Join(dirs.SnapAppArmorDir, "snap.samba.smbd")
		c.Check(profile, testutil.FileContains, scenario.overlaySnippet)
		c.Check(profile, testutil.FileContains, scenario.nfsSnippet)
		updateNSProfile := filepath.Join(dirs.SnapAppArmorDir, "snap-update-ns.samba")
		c.Check(updateNSProfile, testutil.FileContains, scenario.overlaySnippet)
		s.RemoveSnap(c, snapInfo)
	}
}

var casperOverlaySnippetsScenarios = []nfsAndOverlaySnippetsScenario{{
	// By default apparmor is enforcing mode.
	opts:           interfaces.ConfinementOptions{},
	overlaySnippet: `"/upper/{,**/}" r,`,
}, {
	// DevMode switches apparmor to non-enforcing (complain) mode.
	opts:           interfaces.ConfinementOptions{DevMode: true},
	overlaySnippet: `"/upper/{,**/}" r,`,
}, {
	// JailMode switches apparmor to enforcing mode even in the presence of DevMode.
	opts:           interfaces.ConfinementOptions{DevMode: true, JailMode: true},
	overlaySnippet: `"/upper/{,**/}" r,`,
}, {
	// Classic confinement (without jailmode) uses apparmor in complain mode by default and ignores all snippets.
	opts:           interfaces.ConfinementOptions{Classic: true},
	overlaySnippet: "",
}, {
	// Classic confinement in JailMode uses enforcing apparmor.
	opts: interfaces.ConfinementOptions{Classic: true, JailMode: true},
	// FIXME: logic in backend.addContent is wrong for this case
	//overlaySnippet: `"/upper/{,**/}" r,`,
	overlaySnippet: "",
}}

func (s *backendSuite) TestCasperOverlaySnippets(c *C) {
	restore := apparmor_sandbox.MockLevel(apparmor_sandbox.Full)
	defer restore()
	restore = osutil.MockIsHomeUsingNFS(func() (bool, error) { return false, nil })
	defer restore()
	restore = osutil.MockIsRootWritableOverlay(func() (string, error) { return "/upper", nil })
	defer restore()
	s.Iface.AppArmorPermanentSlotCallback = func(spec *apparmor.Specification, slot *snap.SlotInfo) error {
		return nil
	}

	for _, scenario := range casperOverlaySnippetsScenarios {
		snapInfo := s.InstallSnap(c, scenario.opts, "", ifacetest.SambaYamlV1, 1)
		profile := filepath.Join(dirs.SnapAppArmorDir, "snap.samba.smbd")
		c.Check(profile, testutil.FileContains, scenario.overlaySnippet)
		s.RemoveSnap(c, snapInfo)
	}
}

func (s *backendSuite) TestProfileGlobs(c *C) {
	globs := apparmor.ProfileGlobs("foo")
	c.Assert(globs, DeepEquals, []string{"snap.foo.*", "snap-update-ns.foo"})
}

func (s *backendSuite) TestNsProfile(c *C) {
	c.Assert(apparmor.NsProfile("foo"), Equals, "snap-update-ns.foo")
}

func (s *backendSuite) TestSandboxFeatures(c *C) {
	restore := apparmor_sandbox.MockLevel(apparmor_sandbox.Full)
	defer restore()
	restore = apparmor.MockKernelFeatures(func() ([]string, error) { return []string{"foo", "bar"}, nil })
	defer restore()
	restore = apparmor.MockParserFeatures(func() ([]string, error) { return []string{"baz", "norf"}, nil })
	defer restore()

	c.Assert(s.Backend.SandboxFeatures(), DeepEquals, []string{"kernel:foo", "kernel:bar", "parser:baz", "parser:norf", "support-level:full", "policy:default"})
}

func (s *backendSuite) TestSandboxFeaturesPartial(c *C) {
	restore := apparmor_sandbox.MockLevel(apparmor_sandbox.Partial)
	defer restore()
	restore = release.MockReleaseInfo(&release.OS{ID: "opensuse-tumbleweed"})
	defer restore()
	restore = osutil.MockKernelVersion("4.16.10-1-default")
	defer restore()
	restore = apparmor.MockKernelFeatures(func() ([]string, error) { return []string{"foo", "bar"}, nil })
	defer restore()
	restore = apparmor.MockParserFeatures(func() ([]string, error) { return []string{"baz", "norf"}, nil })
	defer restore()

	c.Assert(s.Backend.SandboxFeatures(), DeepEquals, []string{"kernel:foo", "kernel:bar", "parser:baz", "parser:norf", "support-level:partial", "policy:default"})

	restore = osutil.MockKernelVersion("4.14.1-default")
	defer restore()

	c.Assert(s.Backend.SandboxFeatures(), DeepEquals, []string{"kernel:foo", "kernel:bar", "parser:baz", "parser:norf", "support-level:partial", "policy:default"})
}

func (s *backendSuite) TestParallelInstanceSetupSnapUpdateNS(c *C) {
	dirs.SetRootDir(s.RootDir)

	const trivialSnapYaml = `name: some-snap
version: 1.0
apps:
  app:
    command: app-command
`
	snapInfo := snaptest.MockInfo(c, trivialSnapYaml, &snap.SideInfo{Revision: snap.R(222)})
	snapInfo.InstanceKey = "instance"

	s.InstallSnap(c, interfaces.ConfinementOptions{}, "some-snap_instance", trivialSnapYaml, 1)
	profileUpdateNS := filepath.Join(dirs.SnapAppArmorDir, "snap-update-ns.some-snap_instance")
	c.Check(profileUpdateNS, testutil.FileContains, `profile snap-update-ns.some-snap_instance (`)
	c.Check(profileUpdateNS, testutil.FileContains, `
  # Allow parallel instance snap mount namespace adjustments
  mount options=(rw rbind) /snap/some-snap_instance/ -> /snap/some-snap/,
  mount options=(rw rbind) /var/snap/some-snap_instance/ -> /var/snap/some-snap/,
`)
}

func (s *backendSuite) TestPtraceTraceRule(c *C) {
	restoreTemplate := apparmor.MockTemplate("template\n###SNIPPETS###\n")
	defer restoreTemplate()
	restore := apparmor_sandbox.MockLevel(apparmor_sandbox.Full)
	defer restore()
	restore = osutil.MockIsHomeUsingNFS(func() (bool, error) { return false, nil })
	defer restore()

	needle := `deny ptrace (trace),`
	for _, tc := range []struct {
		opts     interfaces.ConfinementOptions
		uses     bool
		suppress bool
		expected bool
	}{
		// strict, only suppress if suppress == true and uses == false
		{
			opts:     interfaces.ConfinementOptions{},
			uses:     false,
			suppress: false,
			expected: false,
		},
		{
			opts:     interfaces.ConfinementOptions{},
			uses:     false,
			suppress: true,
			expected: true,
		},
		{
			opts:     interfaces.ConfinementOptions{},
			uses:     true,
			suppress: false,
			expected: false,
		},
		{
			opts:     interfaces.ConfinementOptions{},
			uses:     true,
			suppress: true,
			expected: false,
		},
		// devmode, only suppress if suppress == true and uses == false
		{
			opts:     interfaces.ConfinementOptions{DevMode: true},
			uses:     false,
			suppress: false,
			expected: false,
		},
		{
			opts:     interfaces.ConfinementOptions{DevMode: true},
			uses:     false,
			suppress: true,
			expected: true,
		},
		{
			opts:     interfaces.ConfinementOptions{DevMode: true},
			uses:     true,
			suppress: false,
			expected: false,
		},
		{
			opts:     interfaces.ConfinementOptions{DevMode: true},
			uses:     true,
			suppress: true,
			expected: false,
		},
		// classic, never suppress
		{
			opts:     interfaces.ConfinementOptions{Classic: true},
			uses:     false,
			suppress: false,
			expected: false,
		},
		{
			opts:     interfaces.ConfinementOptions{Classic: true},
			uses:     false,
			suppress: true,
			expected: false,
		},
		{
			opts:     interfaces.ConfinementOptions{Classic: true},
			uses:     true,
			suppress: false,
			expected: false,
		},
		{
			opts:     interfaces.ConfinementOptions{Classic: true},
			uses:     true,
			suppress: true,
			expected: false,
		},
		// classic with jail, only suppress if suppress == true and uses == false
		{
			opts:     interfaces.ConfinementOptions{Classic: true, JailMode: true},
			uses:     false,
			suppress: false,
			expected: false,
		},
		{
			opts:     interfaces.ConfinementOptions{Classic: true, JailMode: true},
			uses:     false,
			suppress: true,
			expected: true,
		},
		{
			opts:     interfaces.ConfinementOptions{Classic: true, JailMode: true},
			uses:     true,
			suppress: false,
			expected: false,
		},
		{
			opts:     interfaces.ConfinementOptions{Classic: true, JailMode: true},
			uses:     true,
			suppress: true,
			expected: false,
		},
	} {
		s.Iface.AppArmorPermanentSlotCallback = func(spec *apparmor.Specification, slot *snap.SlotInfo) error {
			if tc.uses {
				spec.SetUsesPtraceTrace()
			}
			if tc.suppress {
				spec.SetSuppressPtraceTrace()
			}
			return nil
		}

		snapInfo := s.InstallSnap(c, tc.opts, "", ifacetest.SambaYamlV1, 1)
		appSet := interfaces.NewSnapAppSet(snapInfo)

		err := s.Backend.Setup(appSet, tc.opts, s.Repo, s.meas)
		c.Assert(err, IsNil)

		profile := filepath.Join(dirs.SnapAppArmorDir, "snap.samba.smbd")
		data, err := ioutil.ReadFile(profile)
		c.Assert(err, IsNil)

		if tc.expected {
			c.Assert(string(data), testutil.Contains, needle)
		} else {
			c.Assert(string(data), Not(testutil.Contains), needle)
		}
		s.RemoveSnap(c, snapInfo)
	}
}

func (s *backendSuite) TestHomeIxRule(c *C) {
	restoreTemplate := apparmor.MockTemplate("template\n###SNIPPETS###\nneedle rwkl###HOME_IX###,\n")
	defer restoreTemplate()
	restore := apparmor_sandbox.MockLevel(apparmor_sandbox.Full)
	defer restore()
	restore = osutil.MockIsHomeUsingNFS(func() (bool, error) { return false, nil })
	defer restore()

	for _, tc := range []struct {
		opts     interfaces.ConfinementOptions
		suppress bool
		expected string
	}{
		{
			opts:     interfaces.ConfinementOptions{},
			suppress: true,
			expected: "needle rwkl,",
		},
		{
			opts:     interfaces.ConfinementOptions{},
			suppress: false,
			expected: "needle rwklix,",
		},
	} {
		s.Iface.AppArmorPermanentSlotCallback = func(spec *apparmor.Specification, slot *snap.SlotInfo) error {
			if tc.suppress {
				spec.SetSuppressHomeIx()
			}
			spec.AddSnippet("needle rwkl###HOME_IX###,")
			return nil
		}

		snapInfo := s.InstallSnap(c, tc.opts, "", ifacetest.SambaYamlV1, 1)
		profile := filepath.Join(dirs.SnapAppArmorDir, "snap.samba.smbd")
		data, err := ioutil.ReadFile(profile)
		c.Assert(err, IsNil)

		c.Assert(string(data), testutil.Contains, tc.expected)
		s.RemoveSnap(c, snapInfo)
	}
}

func (s *backendSuite) TestPycacheDenyRule(c *C) {
	restoreTemplate := apparmor.MockTemplate("template\n###PYCACHEDENY###\n")
	defer restoreTemplate()
	restore := apparmor_sandbox.MockLevel(apparmor_sandbox.Full)
	defer restore()
	restore = osutil.MockIsHomeUsingNFS(func() (bool, error) { return false, nil })
	defer restore()

	for _, tc := range []struct {
		opts     interfaces.ConfinementOptions
		suppress bool
		expected Checker
	}{
		{
			opts:     interfaces.ConfinementOptions{},
			suppress: true,
			expected: Not(testutil.Contains),
		},
		{
			opts:     interfaces.ConfinementOptions{},
			suppress: false,
			expected: testutil.Contains,
		},
	} {
		s.Iface.AppArmorPermanentSlotCallback = func(spec *apparmor.Specification, slot *snap.SlotInfo) error {
			if tc.suppress {
				spec.SetSuppressPycacheDeny()
			}
			return nil
		}

		snapInfo := s.InstallSnap(c, tc.opts, "", ifacetest.SambaYamlV1, 1)
		profile := filepath.Join(dirs.SnapAppArmorDir, "snap.samba.smbd")
		data, err := ioutil.ReadFile(profile)
		c.Assert(err, IsNil)

		c.Assert(string(data), tc.expected, "deny /usr/lib/python3*/{,**/}__pycache__/ w,")
		s.RemoveSnap(c, snapInfo)
	}
}

func (s *backendSuite) TestSystemUsernamesPolicy(c *C) {
	restoreTemplate := apparmor.MockTemplate("template\n###SNIPPETS###\n")
	defer restoreTemplate()
	restore := apparmor_sandbox.MockLevel(apparmor_sandbox.Full)
	defer restore()

	snapYaml := `
name: app
version: 0.1
system-usernames:
  testid: shared
apps:
  cmd:
`

	snapInfo := s.InstallSnap(c, interfaces.ConfinementOptions{}, "", snapYaml, 1)
	profile := filepath.Join(dirs.SnapAppArmorDir, "snap.app.cmd")
	data, err := ioutil.ReadFile(profile)
	c.Assert(err, IsNil)
	c.Assert(string(data), testutil.Contains, "capability setuid,")
	c.Assert(string(data), testutil.Contains, "capability setgid,")
	c.Assert(string(data), testutil.Contains, "capability chown,")
	s.RemoveSnap(c, snapInfo)
}

func (s *backendSuite) TestNoSystemUsernamesPolicy(c *C) {
	restoreTemplate := apparmor.MockTemplate("template\n###SNIPPETS###\n")
	defer restoreTemplate()
	restore := apparmor_sandbox.MockLevel(apparmor_sandbox.Full)
	defer restore()

	snapYaml := `
name: app
version: 0.1
apps:
  cmd:
`

	snapInfo := s.InstallSnap(c, interfaces.ConfinementOptions{}, "", snapYaml, 1)
	profile := filepath.Join(dirs.SnapAppArmorDir, "snap.app.cmd")
	data, err := ioutil.ReadFile(profile)
	c.Assert(err, IsNil)
	c.Assert(string(data), Not(testutil.Contains), "capability setuid,")
	c.Assert(string(data), Not(testutil.Contains), "capability setgid,")
	c.Assert(string(data), Not(testutil.Contains), "capability chown,")
	s.RemoveSnap(c, snapInfo)
}

func (s *backendSuite) TestSetupManySmoke(c *C) {
	setupManyInterface, ok := s.Backend.(interfaces.SecurityBackendSetupMany)
	c.Assert(ok, Equals, true)
	c.Assert(setupManyInterface, NotNil)
}

func (s *backendSuite) TestInstallingSnapInPreseedMode(c *C) {
	// Intercept the /proc/self/exe symlink and point it to the snapd from the
	// mounted core snap. This indicates that snapd has re-executed and
	// should not reload snap-confine policy.
	fakeExe := filepath.Join(s.RootDir, "fake-proc-self-exe")
	err := os.Symlink(filepath.Join(dirs.SnapMountDir, "/core/1234/usr/lib/snapd/snapd"), fakeExe)
	c.Assert(err, IsNil)
	restore := apparmor.MockProcSelfExe(fakeExe)
	defer restore()

	aa, ok := s.Backend.(*apparmor.Backend)
	c.Assert(ok, Equals, true)

	opts := interfaces.SecurityBackendOptions{Preseed: true}
	c.Assert(aa.Initialize(&opts), IsNil)

	s.InstallSnap(c, interfaces.ConfinementOptions{}, "", ifacetest.SambaYamlV1, 1)

	updateNSProfile := filepath.Join(dirs.SnapAppArmorDir, "snap-update-ns.samba")
	profile := filepath.Join(dirs.SnapAppArmorDir, "snap.samba.smbd")
	// file called "snap.sambda.smbd" was created
	_, err = os.Stat(profile)
	c.Check(err, IsNil)
	// apparmor_parser was used to load that file
	c.Check(s.loadProfilesCalls, DeepEquals, []loadProfilesParams{
		{
			[]string{updateNSProfile, profile},
			fmt.Sprintf("%s/var/cache/apparmor", s.RootDir),
			apparmor_sandbox.SkipReadCache | apparmor_sandbox.SkipKernelLoad,
		},
	})
}

func (s *backendSuite) TestSetupManyInPreseedMode(c *C) {
	aa, ok := s.Backend.(*apparmor.Backend)
	c.Assert(ok, Equals, true)

	opts := interfaces.SecurityBackendOptions{
		Preseed:      true,
		CoreSnapInfo: ifacetest.DefaultInitializeOpts.CoreSnapInfo,
	}
	c.Assert(aa.Initialize(&opts), IsNil)

	for _, opts := range testedConfinementOpts {
		snapInfo1 := s.InstallSnap(c, opts, "", ifacetest.SambaYamlV1, 1)
		snapInfo2 := s.InstallSnap(c, opts, "", ifacetest.SomeSnapYamlV1, 1)
		appSet1 := interfaces.NewSnapAppSet(snapInfo1)
		appSet2 := interfaces.NewSnapAppSet(snapInfo2)
		s.loadProfilesCalls = nil

		snap1nsProfile := filepath.Join(dirs.SnapAppArmorDir, "snap-update-ns.samba")
		snap1AAprofile := filepath.Join(dirs.SnapAppArmorDir, "snap.samba.smbd")
		snap2nsProfile := filepath.Join(dirs.SnapAppArmorDir, "snap-update-ns.some-snap")
		snap2AAprofile := filepath.Join(dirs.SnapAppArmorDir, "snap.some-snap.someapp")

		// simulate outdated profiles by changing their data on the disk
		c.Assert(os.WriteFile(snap1AAprofile, []byte("# an outdated profile"), 0644), IsNil)
		c.Assert(os.WriteFile(snap2AAprofile, []byte("# an outdated profile"), 0644), IsNil)

		setupManyInterface, ok := s.Backend.(interfaces.SecurityBackendSetupMany)
		c.Assert(ok, Equals, true)
		err := setupManyInterface.SetupMany([]*interfaces.SnapAppSet{appSet1, appSet2}, func(snapName string) interfaces.ConfinementOptions { return opts }, s.Repo, s.meas)
		c.Assert(err, IsNil)

		// expect two batch executions - one for changed profiles, second for unchanged profiles.
		c.Check(s.loadProfilesCalls, DeepEquals, []loadProfilesParams{
			{
				[]string{snap1AAprofile, snap2AAprofile},
				fmt.Sprintf("%s/var/cache/apparmor", s.RootDir),
				apparmor_sandbox.SkipReadCache | apparmor_sandbox.ConserveCPU | apparmor_sandbox.SkipKernelLoad,
			},
			{
				[]string{snap1nsProfile, snap2nsProfile},
				fmt.Sprintf("%s/var/cache/apparmor", s.RootDir),
				apparmor_sandbox.ConserveCPU | apparmor_sandbox.SkipKernelLoad,
			},
		})
		s.RemoveSnap(c, snapInfo1)
		s.RemoveSnap(c, snapInfo2)
	}
}

func (s *backendSuite) TestCoreSnippetOnCoreSystem(c *C) {
	dirs.SetRootDir(s.RootDir)

	// NOTE: replace the real template with a shorter variant
	restoreTemplate := apparmor.MockTemplate("\n" +
		"###SNIPPETS###\n" +
		"\n")
	defer restoreTemplate()

	expectedContents := `
# Allow each snaps to access each their own folder on the
# ubuntu-save partition, with write permissions.
/var/lib/snapd/save/snap/@{SNAP_INSTANCE_NAME}/ rw,
/var/lib/snapd/save/snap/@{SNAP_INSTANCE_NAME}/** mrwklix,
`

	tests := []struct {
		onClassic            bool
		classicConfinement   bool
		jailMode             bool
		shouldContainSnippet bool
	}{
		// XXX: Is it possible for someone to make this nicer?
		{onClassic: false, classicConfinement: false, jailMode: false, shouldContainSnippet: true},
		{onClassic: false, classicConfinement: false, jailMode: true, shouldContainSnippet: true},

		// Rest of the cases the core-specific snippet shouldn't turn up.
		{onClassic: false, classicConfinement: true, jailMode: false, shouldContainSnippet: false},
		{onClassic: false, classicConfinement: true, jailMode: true, shouldContainSnippet: false},
		{onClassic: true, classicConfinement: false, jailMode: false, shouldContainSnippet: false},
		{onClassic: true, classicConfinement: true, jailMode: false, shouldContainSnippet: false},
		{onClassic: true, classicConfinement: false, jailMode: true, shouldContainSnippet: false},
		{onClassic: true, classicConfinement: true, jailMode: true, shouldContainSnippet: false},
	}

	for _, t := range tests {
		restore := release.MockOnClassic(t.onClassic)
		defer restore()

		opts := interfaces.ConfinementOptions{
			Classic:  t.classicConfinement,
			JailMode: t.jailMode,
		}
		snapInfo := s.InstallSnap(c, opts, "", ifacetest.SambaYamlV1, 1)
		profile := filepath.Join(dirs.SnapAppArmorDir, "snap.samba.smbd")
		if t.shouldContainSnippet {
			c.Check(profile, testutil.FileContains, expectedContents, Commentf("Classic %t, JailMode %t", t.onClassic, t.jailMode))
		} else {
			c.Check(profile, Not(testutil.FileContains), expectedContents, Commentf("Classic %t, JailMode %t", t.onClassic, t.jailMode))
		}
		stat, err := os.Stat(profile)
		c.Assert(err, IsNil)
		c.Check(stat.Mode(), Equals, os.FileMode(0644))
		s.RemoveSnap(c, snapInfo)
	}
}
func (s *backendSuite) TestRemoveAllSnapAppArmorProfiles(c *C) {
	dirs.SetRootDir(s.RootDir)

	opts := interfaces.ConfinementOptions{}
	snapInfo1 := s.InstallSnap(c, opts, "", ifacetest.SambaYamlV1, 1)
	s.AddCleanup(func() { s.RemoveSnap(c, snapInfo1) })
	snapInfo2 := s.InstallSnap(c, opts, "", ifacetest.SomeSnapYamlV1, 1)
	s.AddCleanup(func() { s.RemoveSnap(c, snapInfo2) })

	snap1nsProfile := filepath.Join(dirs.SnapAppArmorDir, "snap-update-ns.samba")
	snap1AAprofile := filepath.Join(dirs.SnapAppArmorDir, "snap.samba.smbd")
	snap2nsProfile := filepath.Join(dirs.SnapAppArmorDir, "snap-update-ns.some-snap")
	snap2AAprofile := filepath.Join(dirs.SnapAppArmorDir, "snap.some-snap.someapp")

	for _, p := range []string{snap1nsProfile, snap1AAprofile, snap2nsProfile, snap2AAprofile} {
		_, err := os.Stat(p)
		c.Assert(err, IsNil)
	}

	err := apparmor.RemoveAllSnapAppArmorProfiles()
	c.Assert(err, IsNil)

	for _, p := range []string{snap1nsProfile, snap1AAprofile, snap2nsProfile, snap2AAprofile} {
		_, err := os.Stat(p)
		c.Check(os.IsNotExist(err), Equals, true)
	}
}
