// -*- Mode: Go; indent-tabs-mode: t -*-
//go:build !nomanagers

/*
 * Copyright (C) 2016 Canonical Ltd
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

package configstate_test

import (
	"fmt"
	"time"

	. "gopkg.in/check.v1"

	"github.com/ddkwork/golibrary/mylog"
	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/features"
	"github.com/snapcore/snapd/gadget"
	"github.com/snapcore/snapd/overlord"
	"github.com/snapcore/snapd/overlord/configstate"
	"github.com/snapcore/snapd/overlord/configstate/config"
	"github.com/snapcore/snapd/overlord/configstate/configcore"
	"github.com/snapcore/snapd/overlord/hookstate"
	"github.com/snapcore/snapd/overlord/snapstate"
	"github.com/snapcore/snapd/overlord/snapstate/snapstatetest"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/sysconfig"
	"github.com/snapcore/snapd/testutil"
)

type tasksetsSuite struct {
	state *state.State
}

var (
	_ = Suite(&tasksetsSuite{})
	_ = Suite(&configcoreHijackSuite{})
	_ = Suite(&miscSuite{})
	_ = Suite(&earlyConfigSuite{})
)

func (s *tasksetsSuite) SetUpTest(c *C) {
	s.state = state.New(nil)
}

var configureTests = []struct {
	patch       map[string]interface{}
	optional    bool
	ignoreError bool
	useDefaults bool
}{{
	patch:       nil,
	optional:    true,
	ignoreError: false,
}, {
	patch:       map[string]interface{}{},
	optional:    true,
	ignoreError: false,
}, {
	patch:       map[string]interface{}{"foo": "bar"},
	optional:    false,
	ignoreError: false,
}, {
	patch:       nil,
	optional:    true,
	ignoreError: true,
}, {
	patch:       nil,
	optional:    true,
	ignoreError: true,
	useDefaults: true,
}}

func (s *tasksetsSuite) TestConfigureInstalled(c *C) {
	s.state.Lock()
	snapstate.Set(s.state, "test-snap", &snapstate.SnapState{
		Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{
			{RealName: "test-snap", Revision: snap.R(1)},
		}),
		Current:  snap.R(1),
		Active:   true,
		SnapType: "app",
	})
	s.state.Unlock()

	for _, test := range configureTests {
		var flags int
		if test.ignoreError {
			flags |= snapstate.IgnoreHookError
		}
		if test.useDefaults {
			flags |= snapstate.UseConfigDefaults
		}

		s.state.Lock()
		taskset := configstate.Configure(s.state, "test-snap", test.patch, flags)
		s.state.Unlock()

		tasks := taskset.Tasks()
		c.Assert(tasks, HasLen, 1)
		task := tasks[0]

		c.Assert(task.Kind(), Equals, "run-hook")

		summary := `Run configure hook of "test-snap" snap`
		if test.optional {
			summary += " if present"
		}
		c.Assert(task.Summary(), Equals, summary)

		var hooksup hookstate.HookSetup
		s.state.Lock()
		mylog.Check(task.Get("hook-setup", &hooksup))
		s.state.Unlock()
		c.Check(err, IsNil)

		c.Assert(hooksup.Snap, Equals, "test-snap")
		c.Assert(hooksup.Hook, Equals, "configure")
		c.Assert(hooksup.Optional, Equals, test.optional)
		c.Assert(hooksup.IgnoreError, Equals, test.ignoreError)
		c.Assert(hooksup.Timeout, Equals, 5*time.Minute)

		context := mylog.Check2(hookstate.NewContext(task, task.State(), &hooksup, nil, ""))
		c.Check(err, IsNil)
		c.Check(context.InstanceName(), Equals, "test-snap")
		c.Check(context.SnapRevision(), Equals, snap.Revision{})
		c.Check(context.HookName(), Equals, "configure")

		var patch map[string]interface{}
		var useDefaults bool
		context.Lock()
		context.Get("use-defaults", &useDefaults)
		mylog.Check(context.Get("patch", &patch))
		context.Unlock()
		if len(test.patch) > 0 {
			c.Check(err, IsNil)
			c.Check(patch, DeepEquals, test.patch)
		} else {
			c.Check(err, testutil.ErrorIs, state.ErrNoState)
			c.Check(patch, IsNil)
		}
		c.Check(useDefaults, Equals, test.useDefaults)
	}
}

func (s *tasksetsSuite) TestConfigureInstalledConflict(c *C) {
	s.state.Lock()
	defer s.state.Unlock()
	snapstate.Set(s.state, "test-snap", &snapstate.SnapState{
		Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{
			{RealName: "test-snap", Revision: snap.R(1)},
		}),
		Current:  snap.R(1),
		Active:   true,
		SnapType: "app",
	})

	ts := mylog.Check2(snapstate.Disable(s.state, "test-snap"))

	chg := s.state.NewChange("other-change", "...")
	chg.AddAll(ts)

	patch := map[string]interface{}{"foo": "bar"}
	_ = mylog.Check2(configstate.ConfigureInstalled(s.state, "test-snap", patch, 0))
	c.Check(err, ErrorMatches, `snap "test-snap" has "other-change" change in progress`)
}

func (s *tasksetsSuite) TestConfigureNotInstalled(c *C) {
	patch := map[string]interface{}{"foo": "bar"}
	s.state.Lock()
	defer s.state.Unlock()

	_ := mylog.Check2(configstate.ConfigureInstalled(s.state, "test-snap", patch, 0))
	c.Check(err, ErrorMatches, `snap "test-snap" is not installed`)

	// core can be configure before being installed
	_ = mylog.Check2(configstate.ConfigureInstalled(s.state, "core", patch, 0))
	c.Check(err, IsNil)
}

func (s *tasksetsSuite) TestConfigureInstalledDenyBases(c *C) {
	patch := map[string]interface{}{"foo": "bar"}
	s.state.Lock()
	defer s.state.Unlock()
	snapstate.Set(s.state, "test-base", &snapstate.SnapState{
		Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{
			{RealName: "test-base", Revision: snap.R(1)},
		}),
		Current:  snap.R(1),
		Active:   true,
		SnapType: "base",
	})

	_ := mylog.Check2(configstate.ConfigureInstalled(s.state, "test-base", patch, 0))
	c.Check(err, ErrorMatches, `cannot configure snap "test-base" because it is of type 'base'`)
}

func (s *tasksetsSuite) TestConfigureInstalledDenySnapd(c *C) {
	patch := map[string]interface{}{"foo": "bar"}
	s.state.Lock()
	defer s.state.Unlock()
	snapstate.Set(s.state, "snapd", &snapstate.SnapState{
		Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{
			{RealName: "snapd", Revision: snap.R(1)},
		}),
		Current:  snap.R(1),
		Active:   true,
		SnapType: "snapd",
	})

	_ := mylog.Check2(configstate.ConfigureInstalled(s.state, "snapd", patch, 0))
	c.Check(err, ErrorMatches, `cannot configure the "snapd" snap, please use "system" instead`)
}

func (s *tasksetsSuite) TestDefaultConfigure(c *C) {
	s.state.Lock()
	snapstate.Set(s.state, "test-snap", &snapstate.SnapState{
		Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{
			{RealName: "test-snap", Revision: snap.R(1)},
		}),
		Current:  snap.R(1),
		Active:   true,
		SnapType: "app",
	})

	taskset := configstate.DefaultConfigure(s.state, "test-snap")
	s.state.Unlock()

	tasks := taskset.Tasks()
	c.Assert(tasks, HasLen, 1)
	task := tasks[0]
	c.Check(task.Kind(), Equals, "run-hook")
	c.Check(task.Summary(), Equals, `Run default-configure hook of "test-snap" snap if present`)

	var hooksup hookstate.HookSetup
	s.state.Lock()
	mylog.Check(task.Get("hook-setup", &hooksup))
	s.state.Unlock()

	expectedHookSetup := hookstate.HookSetup{
		Snap:        "test-snap",
		Revision:    snap.Revision{},
		Hook:        "default-configure",
		Timeout:     5 * time.Minute,
		Optional:    true,
		Always:      false,
		IgnoreError: false,
	}
	c.Assert(hooksup, DeepEquals, expectedHookSetup)

	context := mylog.Check2(hookstate.NewContext(task, task.State(), &hooksup, nil, ""))
	c.Check(err, IsNil)
	c.Check(context.InstanceName(), Equals, "test-snap")
	c.Check(context.SnapRevision(), Equals, snap.Revision{})
	c.Check(context.HookName(), Equals, "default-configure")

	var patch map[string]interface{}
	var useDefaults bool
	context.Lock()
	context.Get("use-defaults", &useDefaults)
	mylog.Check(context.Get("patch", &patch))
	context.Unlock()
	// useDefaults is not set because it is implicit to the default-configure hook
	c.Check(useDefaults, Equals, false)
	c.Check(err, testutil.ErrorIs, state.ErrNoState)
	c.Check(patch, IsNil)
}

type configcoreHijackSuite struct {
	testutil.BaseTest

	o     *overlord.Overlord
	state *state.State
}

func (s *configcoreHijackSuite) SetUpTest(c *C) {
	s.BaseTest.SetUpTest(c)
	s.o = overlord.Mock()
	s.state = s.o.State()
	hookMgr := mylog.Check2(hookstate.Manager(s.state, s.o.TaskRunner()))

	s.o.AddManager(hookMgr)
	r := configstate.MockConfigcoreExportExperimentalFlags(func(_ configcore.ConfGetter) error {
		return nil
	})
	s.AddCleanup(r)
	mylog.Check(configstate.Init(s.state, hookMgr))

	s.o.AddManager(s.o.TaskRunner())

	r = snapstatetest.MockDeviceModel(makeModel(nil))
	s.AddCleanup(r)
}

func (s *configcoreHijackSuite) TestConfigMngrInitHomeDirs(c *C) {
	s.o = overlord.Mock()
	s.state = s.o.State()
	hookMgr := mylog.Check2(hookstate.Manager(s.state, s.o.TaskRunner()))

	s.state.Lock()
	t := config.NewTransaction(s.state)
	c.Assert(t.Set("core", "homedirs", "/home,/home/department,/users,/users/seniors"), IsNil)
	t.Commit()
	s.state.Unlock()
	mylog.Check(configstate.Init(s.state, hookMgr))

	snapHomeDirs := []string{"/home", "/home/department", "/users", "/users/seniors"}
	c.Check(dirs.SnapHomeDirs(), DeepEquals, snapHomeDirs)
}

type witnessManager struct {
	state     *state.State
	committed bool
}

func (wm *witnessManager) Ensure() error {
	wm.state.Lock()
	defer wm.state.Unlock()
	t := config.NewTransaction(wm.state)
	var witnessCfg bool
	t.GetMaybe("core", "witness", &witnessCfg)
	if witnessCfg {
		wm.committed = true
	}
	return nil
}

func (s *configcoreHijackSuite) TestHijack(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	ts := configstate.Configure(s.state, "core", map[string]interface{}{
		"witness": true,
	}, 0)
	c.Assert(len(ts.Tasks()), Equals, 1)
	taskID := ts.Tasks()[0].ID()

	configcoreRan := false
	witnessCfg := false
	witnessConfigcoreRun := func(dev sysconfig.Device, conf configcore.RunTransaction) error {
		// called with no state lock!
		conf.State().Lock()
		defer conf.State().Unlock()
		mylog.Check(conf.Get("core", "witness", &witnessCfg))

		configcoreRan = true
		c.Assert(conf.Task().ID(), Equals, taskID)
		return nil
	}
	r := configstate.MockConfigcoreRun(witnessConfigcoreRun)
	defer r()

	witnessMgr := &witnessManager{
		state: s.state,
	}
	s.o.AddManager(witnessMgr)

	chg := s.state.NewChange("configure-core", "configure core")
	chg.AddAll(ts)

	// this will be run by settle helper once no more Ensure are
	// scheduled, the witnessMgr Ensure would not see the
	// committed config unless an additional Ensure Loop is
	// scheduled when committing the configuration
	observe := func() {
		c.Check(witnessCfg, Equals, true)
		c.Check(witnessMgr.committed, Equals, true)
	}

	s.state.Unlock()
	mylog.Check(s.o.SettleObserveBeforeCleanups(5*time.Second, observe))
	s.state.Lock()


	c.Check(chg.Err(), IsNil)
	c.Check(configcoreRan, Equals, true)
}

type miscSuite struct{}

func (s *miscSuite) TestRemappingFuncs(c *C) {
	// We don't change those.
	c.Assert(configstate.RemapSnapFromRequest("foo"), Equals, "foo")
	c.Assert(configstate.RemapSnapFromRequest("snapd"), Equals, "snapd")
	c.Assert(configstate.RemapSnapFromRequest("core"), Equals, "core")
	c.Assert(configstate.RemapSnapToResponse("foo"), Equals, "foo")
	c.Assert(configstate.RemapSnapToResponse("snapd"), Equals, "snapd")

	// This is the one we alter.
	c.Assert(configstate.RemapSnapFromRequest("system"), Equals, "core")
	c.Assert(configstate.RemapSnapToResponse("core"), Equals, "system")
}

type earlyConfigSuite struct {
	testutil.BaseTest

	state   *state.State
	rootdir string
}

func (s *earlyConfigSuite) SetUpTest(c *C) {
	s.BaseTest.SetUpTest(c)

	s.state = state.New(nil)

	s.rootdir = c.MkDir()

	dirs.SetRootDir(s.rootdir)
	s.AddCleanup(func() { dirs.SetRootDir("") })
}

func (s *earlyConfigSuite) sysConfig(c *C) {
	t := config.NewTransaction(s.state)
	mylog.Check(t.Set("core", "experimental.parallel-instances", true))

	mylog.Check(t.Set("core", "experimental.user-daemons", true))

	t.Commit()
}

func (s *earlyConfigSuite) TestEarlyConfigSeeded(c *C) {
	s.state.Lock()
	defer s.state.Unlock()
	s.sysConfig(c)
	s.state.Set("seeded", true)
	mylog.Check(configstate.EarlyConfig(s.state, nil))

	// parallel-instances was exported
	c.Check(features.ParallelInstances.IsEnabled(), Equals, true)
}

func (s *earlyConfigSuite) TestEarlyConfigSeededErr(c *C) {
	r := configstate.MockConfigcoreExportExperimentalFlags(func(conf configcore.ConfGetter) error {
		return fmt.Errorf("bad bad")
	})
	defer r()

	s.state.Lock()
	defer s.state.Unlock()
	s.state.Set("seeded", true)
	mylog.Check(configstate.EarlyConfig(s.state, nil))
	c.Assert(err, ErrorMatches, "cannot export experimental config flags: bad bad")
}

func (s *earlyConfigSuite) TestEarlyConfigSysConfigured(c *C) {
	s.state.Lock()
	defer s.state.Unlock()
	s.sysConfig(c)

	preloadGadget := func() (sysconfig.Device, *gadget.Info, error) {
		panic("unexpected")
	}
	mylog.Check(configstate.EarlyConfig(s.state, preloadGadget))

	// parallel-instances was exported
	c.Check(features.ParallelInstances.IsEnabled(), Equals, true)
}

const preloadedGadgetYaml = `
defaults:
   system:
     experimental:
       parallel-instances: true
       user-daemons: true
     services:
       ssh.disable
`

func (s *earlyConfigSuite) TestEarlyConfigFromGadget(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	preloadGadget := func() (sysconfig.Device, *gadget.Info, error) {
		gi := mylog.Check2(gadget.InfoFromGadgetYaml([]byte(preloadedGadgetYaml), nil))

		dev := &snapstatetest.TrivialDeviceContext{}
		return dev, gi, nil
	}
	mylog.Check(configstate.EarlyConfig(s.state, preloadGadget))


	// parallel-instances was exported
	c.Check(features.ParallelInstances.IsEnabled(), Equals, true)
	tr := config.NewTransaction(s.state)
	ok := mylog.Check2(features.Flag(tr, features.ParallelInstances))

	c.Check(ok, Equals, true)
	ok = mylog.Check2(features.Flag(tr, features.UserDaemons))

	c.Check(ok, Equals, true)
	var serviceCfg map[string]interface{}
	mylog.Check(tr.Get("core", "services", &serviceCfg))
	// nothing of this was set
	c.Assert(config.IsNoOption(err), Equals, true)
}

func (s *earlyConfigSuite) TestEarlyConfigFromGadgetErr(c *C) {
	defer configstate.MockConfigcoreEarly(func(sysconfig.Device, configcore.RunTransaction, map[string]interface{}) error {
		return fmt.Errorf("boom")
	})()

	s.state.Lock()
	defer s.state.Unlock()

	preloadGadget := func() (sysconfig.Device, *gadget.Info, error) {
		return nil, &gadget.Info{}, nil
	}
	mylog.Check(configstate.EarlyConfig(s.state, preloadGadget))
	c.Assert(err, ErrorMatches, "boom")
}

func (s *earlyConfigSuite) TestEarlyConfigNoHookTask(c *C) {
	defer configstate.MockConfigcoreEarly(func(dev sysconfig.Device, cfg configcore.RunTransaction, vals map[string]interface{}) error {
		c.Assert(cfg.Task(), IsNil)
		return nil
	})()

	s.state.Lock()
	defer s.state.Unlock()

	preloadGadget := func() (sysconfig.Device, *gadget.Info, error) {
		return nil, &gadget.Info{}, nil
	}
	mylog.Check(configstate.EarlyConfig(s.state, preloadGadget))

}

func (s *earlyConfigSuite) TestEarlyConfigPreloadGadgetErr(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	preloadGadget := func() (sysconfig.Device, *gadget.Info, error) {
		return nil, nil, fmt.Errorf("cannot load gadget")
	}
	mylog.Check(configstate.EarlyConfig(s.state, preloadGadget))
	c.Assert(err, ErrorMatches, "cannot load gadget")
}

func (s *earlyConfigSuite) TestEarlyConfigNoGadget(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	preloadGadget := func() (sysconfig.Device, *gadget.Info, error) {
		return nil, nil, state.ErrNoState
	}
	mylog.Check(configstate.EarlyConfig(s.state, preloadGadget))


	sysCfg := mylog.Check2(config.GetSnapConfig(s.state, "core"))

	c.Check(sysCfg, IsNil)
}
