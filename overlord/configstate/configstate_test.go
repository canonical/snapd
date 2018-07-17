// -*- Mode: Go; indent-tabs-mode: t -*-

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
	"time"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/overlord"
	"github.com/snapcore/snapd/overlord/configstate"
	"github.com/snapcore/snapd/overlord/configstate/config"
	"github.com/snapcore/snapd/overlord/configstate/configcore"
	"github.com/snapcore/snapd/overlord/hookstate"
	"github.com/snapcore/snapd/overlord/snapstate"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/snap"
)

type tasksetsSuite struct {
	state *state.State
}

var _ = Suite(&tasksetsSuite{})
var _ = Suite(&configcoreHijackSuite{})

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
		Sequence: []*snap.SideInfo{
			{RealName: "test-snap", Revision: snap.R(1)},
		},
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
		err := task.Get("hook-setup", &hooksup)
		s.state.Unlock()
		c.Check(err, IsNil)

		c.Assert(hooksup.Snap, Equals, "test-snap")
		c.Assert(hooksup.Hook, Equals, "configure")
		c.Assert(hooksup.Optional, Equals, test.optional)
		c.Assert(hooksup.IgnoreError, Equals, test.ignoreError)
		c.Assert(hooksup.Timeout, Equals, 5*time.Minute)

		context, err := hookstate.NewContext(task, task.State(), &hooksup, nil, "")
		c.Check(err, IsNil)
		c.Check(context.SnapName(), Equals, "test-snap")
		c.Check(context.SnapRevision(), Equals, snap.Revision{})
		c.Check(context.HookName(), Equals, "configure")

		var patch map[string]interface{}
		var useDefaults bool
		context.Lock()
		context.Get("use-defaults", &useDefaults)
		err = context.Get("patch", &patch)
		context.Unlock()
		if len(test.patch) > 0 {
			c.Check(err, IsNil)
			c.Check(patch, DeepEquals, test.patch)
		} else {
			c.Check(err, Equals, state.ErrNoState)
			c.Check(patch, IsNil)
		}
		c.Check(useDefaults, Equals, test.useDefaults)
	}
}

func (s *tasksetsSuite) TestConfigureInstalledConflict(c *C) {
	s.state.Lock()
	defer s.state.Unlock()
	snapstate.Set(s.state, "test-snap", &snapstate.SnapState{
		Sequence: []*snap.SideInfo{
			{RealName: "test-snap", Revision: snap.R(1)},
		},
		Current:  snap.R(1),
		Active:   true,
		SnapType: "app",
	})

	ts, err := snapstate.Disable(s.state, "test-snap")
	c.Assert(err, IsNil)
	chg := s.state.NewChange("other-change", "...")
	chg.AddAll(ts)

	patch := map[string]interface{}{"foo": "bar"}
	_, err = configstate.ConfigureInstalled(s.state, "test-snap", patch, 0)
	c.Check(err, ErrorMatches, `snap "test-snap" has "other-change" change in progress`)
}

func (s *tasksetsSuite) TestConfigureNotInstalled(c *C) {
	patch := map[string]interface{}{"foo": "bar"}
	s.state.Lock()
	defer s.state.Unlock()

	_, err := configstate.ConfigureInstalled(s.state, "test-snap", patch, 0)
	c.Check(err, ErrorMatches, `snap "test-snap" is not installed`)

	// core can be configure before being installed
	_, err = configstate.ConfigureInstalled(s.state, "core", patch, 0)
	c.Check(err, IsNil)
}

func (s *tasksetsSuite) TestConfigureDenyBases(c *C) {
	patch := map[string]interface{}{"foo": "bar"}
	s.state.Lock()
	defer s.state.Unlock()
	snapstate.Set(s.state, "test-base", &snapstate.SnapState{
		Sequence: []*snap.SideInfo{
			{RealName: "test-base", Revision: snap.R(1)},
		},
		Current:  snap.R(1),
		Active:   true,
		SnapType: "base",
	})

	_, err := configstate.ConfigureInstalled(s.state, "test-base", patch, 0)
	c.Check(err, ErrorMatches, `cannot configure snap "test-base" because it is of type 'base'`)
}

func (s *tasksetsSuite) TestConfigureDenySnapd(c *C) {
	patch := map[string]interface{}{"foo": "bar"}
	s.state.Lock()
	defer s.state.Unlock()
	snapstate.Set(s.state, "snapd", &snapstate.SnapState{
		Sequence: []*snap.SideInfo{
			{RealName: "snapd", Revision: snap.R(1)},
		},
		Current:  snap.R(1),
		Active:   true,
		SnapType: "app",
	})

	_, err := configstate.ConfigureInstalled(s.state, "snapd", patch, 0)
	c.Check(err, ErrorMatches, `cannot configure the "snapd" snap, please use "system" instead`)
}

type configcoreHijackSuite struct {
	o     *overlord.Overlord
	state *state.State
}

func (s *configcoreHijackSuite) SetUpTest(c *C) {
	s.o = overlord.Mock()
	s.state = s.o.State()
	hookMgr, err := hookstate.Manager(s.state)
	c.Assert(err, IsNil)
	s.o.AddManager(hookMgr)
	configstate.Init(hookMgr)
}

type witnessManager struct {
	state     *state.State
	committed bool
}

func (m *witnessManager) KnownTaskKinds() []string {
	return nil
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

func (wm *witnessManager) Stop() {
}

func (wm *witnessManager) Wait() {
}

func (s *configcoreHijackSuite) TestHijack(c *C) {
	configcoreRan := false
	witnessCfg := false
	witnessConfigcoreRun := func(conf configcore.Conf) error {
		// called with no state lock!
		conf.State().Lock()
		defer conf.State().Unlock()
		err := conf.Get("core", "witness", &witnessCfg)
		c.Assert(err, IsNil)
		configcoreRan = true
		return nil
	}
	r := configstate.MockConfigcoreRun(witnessConfigcoreRun)
	defer r()

	witnessMgr := &witnessManager{
		state: s.state,
	}
	s.o.AddManager(witnessMgr)

	s.state.Lock()
	defer s.state.Unlock()

	ts := configstate.Configure(s.state, "core", map[string]interface{}{
		"witness": true,
	}, 0)

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
	err := s.o.SettleObserveBeforeCleanups(5*time.Second, observe)
	s.state.Lock()
	c.Assert(err, IsNil)

	c.Check(chg.Err(), IsNil)
	c.Check(configcoreRan, Equals, true)
}
