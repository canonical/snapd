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
	"bytes"
	"encoding/json"

	. "gopkg.in/check.v1"

	"github.com/ddkwork/golibrary/mylog"
	"github.com/snapcore/snapd/jsonutil"
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
	buf := mylog.Check2(json.Marshal(nil))

	empty2 := (*json.RawMessage)(&buf)
	// validity check
	c.Check(bytes.Compare(*empty2, []byte(`null`)), Equals, 0)

	for _, emptyCfg := range []*json.RawMessage{nil, &empty1, empty2, {}} {
		rawCfg := mylog.Check2(config.GetSnapConfig(s.state, "snap1"))

		c.Check(rawCfg, IsNil)

		// can set to empty when empty and it's fine
		c.Assert(config.SetSnapConfig(s.state, "snap1", emptyCfg), IsNil)
		rawCfg = mylog.Check2(config.GetSnapConfig(s.state, "snap1"))

		c.Check(rawCfg, IsNil)

		cfg := json.RawMessage(`{"foo":"bar"}`)
		c.Assert(config.SetSnapConfig(s.state, "snap1", &cfg), IsNil)

		// the set sets it
		rawCfg = mylog.Check2(config.GetSnapConfig(s.state, "snap1"))

		c.Assert(rawCfg, NotNil)
		c.Check(*rawCfg, DeepEquals, json.RawMessage(`{"foo":"bar"}`))

		// empty or nil clears it
		c.Assert(config.SetSnapConfig(s.state, "snap1", emptyCfg), IsNil)
		rawCfg = mylog.Check2(config.GetSnapConfig(s.state, "snap1"))

		c.Check(rawCfg, IsNil)

		// and there is no entry for the snap in state
		var config map[string]interface{}
		c.Assert(s.state.Get("config", &config), IsNil)
		_, ok := config["snap1"]
		c.Check(ok, Equals, false)
	}
}

func (s *configHelpersSuite) TestPatchInvalidConfig(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	invalid := []string{}
	value := json.RawMessage([]byte("[]"))
	_ := mylog.Check2(config.PatchConfig("snap1", []string{"foo"}, 0, invalid, &value))
	c.Assert(err, ErrorMatches, `internal error: unexpected configuration type \[\]string`)
}

func (s *configHelpersSuite) TestPurgeNulls(c *C) {
	cfg1 := map[string]interface{}{
		"foo": nil,
		"bar": map[string]interface{}{
			"one": 1,
			"two": nil,
		},
		"baz": map[string]interface{}{
			"three": nil,
		},
	}
	config.PurgeNulls(cfg1)
	c.Check(cfg1, DeepEquals, map[string]interface{}{
		"bar": map[string]interface{}{
			"one": 1,
		},
		"baz": map[string]interface{}{},
	})

	cfg2 := map[string]interface{}{"foo": nil}
	c.Check(config.PurgeNulls(cfg2), DeepEquals, map[string]interface{}{})
	c.Check(cfg2, DeepEquals, map[string]interface{}{})

	jsonData := mylog.Check2(json.Marshal(map[string]interface{}{
		"foo": nil,
		"bar": map[string]interface{}{
			"one": 2,
			"two": nil,
		},
		"baz": map[string]interface{}{
			"three": nil,
		},
	}))

	raw := json.RawMessage(jsonData)
	cfg4 := map[string]*json.RawMessage{
		"root": &raw,
	}
	config.PurgeNulls(cfg4)

	val, ok := cfg4["root"]
	c.Assert(ok, Equals, true)

	var out interface{}
	jsonutil.DecodeWithNumber(bytes.NewReader(*val), &out)
	c.Check(out, DeepEquals, map[string]interface{}{
		"bar": map[string]interface{}{
			"one": json.Number("2"),
		},
		"baz": map[string]interface{}{},
	})

	sub := json.RawMessage(`{"foo":"bar"}`)
	cfg5 := map[string]interface{}{
		"core": map[string]*json.RawMessage{
			"proxy": nil,
			"sub":   &sub,
		},
	}
	config.PurgeNulls(cfg5)
	c.Check(cfg5, DeepEquals, map[string]interface{}{
		"core": map[string]*json.RawMessage{
			"sub": &sub,
		},
	})
}

func (s *configHelpersSuite) TestPurgeNullsTopLevelNull(c *C) {
	cfgJSON := `{
  "experimental": {
    "parallel-instances": true,
    "snapd-snap": true
  },
  "proxy": null,
  "seed": {
    "loaded": true
  }
}`
	var cfg map[string]*json.RawMessage
	mylog.Check(jsonutil.DecodeWithNumber(bytes.NewReader([]byte(cfgJSON)), &cfg))


	config.PurgeNulls(cfg)

	cfgJSON2 := mylog.Check2(json.Marshal(cfg))


	var out interface{}
	jsonutil.DecodeWithNumber(bytes.NewReader(cfgJSON2), &out)
	c.Check(out, DeepEquals, map[string]interface{}{
		"experimental": map[string]interface{}{
			"parallel-instances": true,
			"snapd-snap":         true,
		},
		"seed": map[string]interface{}{
			"loaded": true,
		},
	})
}

func (s *configHelpersSuite) TestSortPatchKeys(c *C) {
	// empty case
	keys := config.SortPatchKeysByDepth(map[string]interface{}{})
	c.Assert(keys, IsNil)

	patch := map[string]interface{}{
		"a.b.c":         0,
		"a":             0,
		"a.b.c.d":       0,
		"q.w.e.r.t.y.u": 0,
		"f.g":           0,
	}

	keys = config.SortPatchKeysByDepth(patch)
	c.Assert(keys, DeepEquals, []string{"a", "f.g", "a.b.c", "a.b.c.d", "q.w.e.r.t.y.u"})
}

func (s *configHelpersSuite) TestPatch(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	s.state.Set("config", map[string]map[string]interface{}{
		"some-snap": {"a": map[string]interface{}{"b": 1}},
	})

	patch := map[string]interface{}{
		"a.b1": 1,
		"a":    map[string]interface{}{},
		"a.b2": map[string]interface{}{"c": "C"},
	}

	tr := config.NewTransaction(s.state)
	mylog.Check(config.Patch(tr, "some-snap", patch))


	var a map[string]interface{}
	mylog.Check(tr.Get("some-snap", "a", &a))
	c.Check(err, IsNil)

	c.Check(a, DeepEquals, map[string]interface{}{
		"b1": json.Number("1"),
		"b2": map[string]interface{}{"c": "C"},
	})
}
