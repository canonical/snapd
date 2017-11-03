// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2017 Canonical Ltd
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
	"reflect"
	"time"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/corecfg"
	"github.com/snapcore/snapd/overlord"
	"github.com/snapcore/snapd/overlord/configstate"
	"github.com/snapcore/snapd/overlord/hookstate"
	"github.com/snapcore/snapd/overlord/snapstate"
	"github.com/snapcore/snapd/overlord/state"
)

type mockConf struct {
	v map[string]interface{}
}

func (cfg *mockConf) Get(snapName, key string, result interface{}) error {
	v := cfg.v[fmt.Sprintf("%s:%s", snapName, key)]

	v1 := reflect.ValueOf(result)
	v2 := reflect.Indirect(v1)
	v2.Set(reflect.ValueOf(v))
	return nil
}

type configmgrSuite struct {
	o     *overlord.Overlord
	state *state.State
	mgr   *configstate.ConfigManager
}

var _ = Suite(&configmgrSuite{})

func (s *configmgrSuite) SetUpTest(c *C) {
	s.o = overlord.Mock()
	s.state = s.o.State()

	hookmgr, err := hookstate.Manager(s.state)
	c.Assert(err, IsNil)

	s.mgr, err = configstate.Manager(s.state, hookmgr)
	c.Assert(err, IsNil)
}

func (s *configmgrSuite) settle() {
	for i := 0; i < 10; i++ {
		s.mgr.Ensure()
		time.Sleep(10 * time.Millisecond)
	}
}

func (s *configmgrSuite) TestConfigureGeneratesConfigureSnapdTask(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	ts := configstate.Configure(s.state, "core", nil, 0)
	c.Check(ts.Tasks(), HasLen, 1)
	c.Check(ts.Tasks()[0].Kind(), Equals, "configure-snapd")
}

func (s *configmgrSuite) TestDoRunCoreConfigureIntegration(c *C) {
	coreCfgRunCalled := false
	restore := configstate.MockCorecfgRun(func(tr corecfg.Conf) error {
		var v string

		coreCfgRunCalled = true
		tr.Get("core", "key", &v)
		c.Check(v, Equals, "value")
		return nil
	})
	defer restore()

	patch := map[string]interface{}{
		"key": "value",
	}
	s.state.Lock()
	defer s.state.Unlock()

	ts := configstate.Configure(s.state, "core", patch, 0)
	chg := s.state.NewChange("corecfg", "configure core")
	chg.AddAll(ts)

	s.state.Unlock()
	s.settle()
	s.state.Lock()
	c.Check(coreCfgRunCalled, Equals, true)
	c.Check(chg.IsReady(), Equals, true)
	c.Check(chg.Err(), IsNil)
}

func (s *configmgrSuite) TestDoRunCoreConfigureWithError(c *C) {
	coreCfgRunCalled := false
	restore := configstate.MockCorecfgRun(func(tr corecfg.Conf) error {
		coreCfgRunCalled = true
		return fmt.Errorf("runCoreCfg fail")
	})
	defer restore()

	s.state.Lock()
	defer s.state.Unlock()

	ts := configstate.Configure(s.state, "core", nil, 0)
	chg := s.state.NewChange("corecfg", "configure core")
	chg.AddAll(ts)

	s.state.Unlock()
	s.settle()
	s.state.Lock()
	c.Check(coreCfgRunCalled, Equals, true)
	c.Check(chg.IsReady(), Equals, true)
	c.Check(chg.Err(), ErrorMatches, `(?sm)cannot perform the following tasks:.*runCoreCfg fail.*`)
}

func (s *configmgrSuite) TestDoRunCoreConfigureWithIgnoreError(c *C) {
	coreCfgRunCalled := false
	restore := configstate.MockCorecfgRun(func(tr corecfg.Conf) error {
		coreCfgRunCalled = true
		return fmt.Errorf("runCoreCfg fail")
	})
	defer restore()

	s.state.Lock()
	defer s.state.Unlock()

	ts := configstate.Configure(s.state, "core", nil, snapstate.IgnoreHookError)
	chg := s.state.NewChange("corecfg", "configure core")
	chg.AddAll(ts)

	s.state.Unlock()
	s.settle()
	s.state.Lock()
	c.Check(coreCfgRunCalled, Equals, true)
	c.Check(chg.IsReady(), Equals, true)
	c.Check(chg.Err(), IsNil)
}
