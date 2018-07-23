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

package config_test

import (
	"encoding/json"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/overlord/configstate/config"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/snap"
)

type configHelpersSuite struct {
	state *state.State
}

var _ = Suite(&configHelpersSuite{})

func (s *configHelpersSuite) SetUpTest(c *C) {
	s.state = state.New(nil)
}

func (s *configHelpersSuite) TestConfigSnapshot(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	tr := config.NewTransaction(s.state)
	c.Assert(tr.Set("snap1", "foo", "a"), IsNil)
	c.Assert(tr.Set("snap2", "bar", "q"), IsNil)
	tr.Commit()

	// store current config
	c.Assert(config.SaveRevisionConfig(s.state, "snap1", snap.R(1)), IsNil)
	c.Assert(config.SaveRevisionConfig(s.state, "snap2", snap.R(7)), IsNil)

	var cfgsnapshot map[string]map[string]map[string]interface{}
	c.Assert(s.state.Get("revision-config", &cfgsnapshot), IsNil)
	c.Assert(cfgsnapshot, DeepEquals, map[string]map[string]map[string]interface{}{
		"snap1": {"1": {"foo": "a"}},
		"snap2": {"7": {"bar": "q"}},
	})

	c.Assert(cfgsnapshot["snap1"], NotNil)

	// modify 'foo' config key
	tr = config.NewTransaction(s.state)
	c.Assert(tr.Set("snap1", "foo", "b"), IsNil)
	tr.Commit()

	// store current config
	c.Assert(config.SaveRevisionConfig(s.state, "snap1", snap.R(2)), IsNil)

	c.Assert(s.state.Get("revision-config", &cfgsnapshot), IsNil)
	c.Assert(cfgsnapshot, DeepEquals, map[string]map[string]map[string]interface{}{
		"snap1": {"1": {"foo": "a"}, "2": {"foo": "b"}},
		"snap2": {"7": {"bar": "q"}},
	})

	var value string

	// Restore first revision
	c.Assert(config.RestoreRevisionConfig(s.state, "snap1", snap.R(1)), IsNil)
	tr = config.NewTransaction(s.state)
	c.Assert(tr.Get("snap1", "foo", &value), IsNil)
	c.Check(value, Equals, "a")

	// Restore second revision
	c.Assert(config.RestoreRevisionConfig(s.state, "snap1", snap.R(2)), IsNil)
	tr = config.NewTransaction(s.state)
	c.Assert(tr.Get("snap1", "foo", &value), IsNil)
	c.Check(value, Equals, "b")
}

func (s *configHelpersSuite) TestDiscardRevisionConfig(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	tr := config.NewTransaction(s.state)
	c.Assert(tr.Set("snap3", "foo", "a"), IsNil)
	tr.Commit()

	for i := 1; i <= 3; i++ {
		c.Assert(config.SaveRevisionConfig(s.state, "snap3", snap.R(i)), IsNil)
	}

	var cfgsnapshot map[string]map[string]interface{}
	c.Assert(s.state.Get("revision-config", &cfgsnapshot), IsNil)
	c.Assert(cfgsnapshot["snap3"], NotNil)
	c.Assert(cfgsnapshot["snap3"], HasLen, 3)

	for i := 1; i <= 2; i++ {
		c.Assert(config.DiscardRevisionConfig(s.state, "snap3", snap.R(i)), IsNil)
	}
	cfgsnapshot = nil
	c.Assert(s.state.Get("revision-config", &cfgsnapshot), IsNil)
	c.Assert(cfgsnapshot["snap3"], NotNil)
	c.Assert(cfgsnapshot["snap3"], HasLen, 1)

	// removing the last revision removes snap completely from the config map
	cfgsnapshot = nil
	c.Assert(config.DiscardRevisionConfig(s.state, "snap3", snap.R(3)), IsNil)
	c.Assert(s.state.Get("revision-config", &cfgsnapshot), IsNil)
	c.Assert(cfgsnapshot["snap3"], IsNil)
}

func (s *configHelpersSuite) TestConfigSnapshotNoConfigs(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	// snap has no config in global state
	c.Assert(config.SaveRevisionConfig(s.state, "snap1", snap.R(1)), IsNil)

	// snap has no config in global state, but config is not nil
	tr := config.NewTransaction(s.state)
	c.Assert(tr.Set("snap2", "bar", "q"), IsNil)
	tr.Commit()
	c.Assert(config.SaveRevisionConfig(s.state, "snap1", snap.R(1)), IsNil)

	// no configuration to restore in revision-config
	c.Assert(config.RestoreRevisionConfig(s.state, "snap1", snap.R(1)), IsNil)
}

func (s *configHelpersSuite) TestSnapConfig(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	empty1 := json.RawMessage(nil)

	for _, emptyCfg := range []*json.RawMessage{nil, &empty1, {}} {
		rawCfg, err := config.GetSnapConfig(s.state, "snap1")
		c.Assert(err, IsNil)
		c.Check(rawCfg, IsNil)

		// can set to empty when empty and it's fine
		c.Assert(config.SetSnapConfig(s.state, "snap1", emptyCfg), IsNil)
		rawCfg, err = config.GetSnapConfig(s.state, "snap1")
		c.Assert(err, IsNil)
		c.Check(rawCfg, IsNil)

		cfg := json.RawMessage(`{"foo":"bar"}`)
		c.Assert(config.SetSnapConfig(s.state, "snap1", &cfg), IsNil)

		// the set sets it
		rawCfg, err = config.GetSnapConfig(s.state, "snap1")
		c.Assert(err, IsNil)
		c.Assert(rawCfg, NotNil)
		c.Check(*rawCfg, DeepEquals, json.RawMessage(`{"foo":"bar"}`))

		// empty or nil clears it
		c.Assert(config.SetSnapConfig(s.state, "snap1", emptyCfg), IsNil)
		rawCfg, err = config.GetSnapConfig(s.state, "snap1")
		c.Assert(err, IsNil)
		c.Check(rawCfg, IsNil)
	}
}
