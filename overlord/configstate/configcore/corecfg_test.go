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

package configcore_test

import (
	"fmt"
	"reflect"
	"testing"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/overlord/configstate/config"
	"github.com/snapcore/snapd/overlord/configstate/configcore"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/systemd"
)

func Test(t *testing.T) { TestingT(t) }

type mockConf struct {
	state   *state.State
	conf    map[string]interface{}
	changes map[string]interface{}
	err     error
}

func (cfg *mockConf) Get(snapName, key string, result interface{}) error {
	if snapName != "core" {
		return fmt.Errorf("mockConf only knows about core")
	}

	var value interface{}
	value = cfg.changes[key]
	if value == nil {
		value = cfg.conf[key]
	}
	if value != nil {
		v1 := reflect.ValueOf(result)
		v2 := reflect.Indirect(v1)
		v2.Set(reflect.ValueOf(value))
	}
	return cfg.err
}

func (cfg *mockConf) GetMaybe(snapName, key string, result interface{}) error {
	err := cfg.Get(snapName, key, result)
	if err != nil && !config.IsNoOption(err) {
		return err
	}
	return nil
}

func (cfg *mockConf) GetPristine(snapName, key string, result interface{}) error {
	if snapName != "core" {
		return fmt.Errorf("mockConf only knows about core")
	}

	var value interface{}
	value = cfg.conf[key]
	if value != nil {
		v1 := reflect.ValueOf(result)
		v2 := reflect.Indirect(v1)
		v2.Set(reflect.ValueOf(value))
	}
	return cfg.err
}

func (cfg *mockConf) GetPristineMaybe(snapName, key string, result interface{}) error {
	err := cfg.GetPristine(snapName, key, result)
	if err != nil && !config.IsNoOption(err) {
		return err
	}
	return nil
}

func (cfg *mockConf) Set(snapName, key string, v interface{}) error {
	if snapName != "core" {
		return fmt.Errorf("mockConf only knows about core")
	}
	if cfg.conf == nil {
		cfg.conf = make(map[string]interface{})
	}
	cfg.conf[key] = v
	return nil
}

func (cfg *mockConf) Changes() []string {
	out := make([]string, 0, len(cfg.changes))
	for k := range cfg.changes {
		out = append(out, "core."+k)
	}
	return out
}

func (cfg *mockConf) State() *state.State {
	return cfg.state
}

// configcoreSuite is the base for all the configcore tests
type configcoreSuite struct {
	state *state.State

	systemctlArgs     [][]string
	systemctlRestorer func()
}

var _ = Suite(&configcoreSuite{})

func (s *configcoreSuite) SetUpSuite(c *C) {
	s.systemctlRestorer = systemd.MockSystemctl(func(args ...string) ([]byte, error) {
		s.systemctlArgs = append(s.systemctlArgs, args[:])
		output := []byte("ActiveState=inactive")
		return output, nil
	})
}

func (s *configcoreSuite) TearDownSuite(c *C) {
	s.systemctlRestorer()
}

func (s *configcoreSuite) SetUpTest(c *C) {
	dirs.SetRootDir(c.MkDir())
	s.state = state.New(nil)
}

func (s *configcoreSuite) TearDownTest(c *C) {
	dirs.SetRootDir("")
}

// runCfgSuite tests configcore.Run()
type runCfgSuite struct {
	configcoreSuite
}

var _ = Suite(&runCfgSuite{})

func (r *runCfgSuite) TestConfigureUnknownOption(c *C) {
	conf := &mockConf{
		state: r.state,
		changes: map[string]interface{}{
			"unknown.option": "1",
		},
	}

	err := configcore.Run(conf)
	c.Check(err, ErrorMatches, `cannot set "core.unknown.option": unsupported system option`)
}

// applyCfgSuite tests configcore.Apply()
type applyCfgSuite struct {
	tmpDir string
}

var _ = Suite(&applyCfgSuite{})

func (s *applyCfgSuite) SetUpTest(c *C) {
	s.tmpDir = c.MkDir()
	dirs.SetRootDir(s.tmpDir)
}

func (s *applyCfgSuite) TearDownTest(c *C) {
	dirs.SetRootDir("")
}

func (s *applyCfgSuite) TestEmptyRootDir(c *C) {
	err := configcore.FilesystemOnlyApply("", nil)
	c.Check(err, ErrorMatches, `internal error: root directory for configcore.FilesystemOnlyApply\(\) not set`)
}

func (s *applyCfgSuite) TestSmoke(c *C) {
	conf := &mockConf{}
	c.Assert(configcore.FilesystemOnlyApply(s.tmpDir, conf), IsNil)
}

func (s *applyCfgSuite) TestPlainCoreConfigGetErrorIfNotCore(c *C) {
	conf := configcore.PlainCoreConfig(map[string]interface{}{})
	var val interface{}
	c.Assert(conf.Get("some-snap", "a", &val), ErrorMatches, `internal error: expected core snap in Get\(\), "some-snap" was requested`)
}

func (s *applyCfgSuite) TestPlainCoreConfigGet(c *C) {
	conf := configcore.PlainCoreConfig(map[string]interface{}{"foo": "bar"})
	var val interface{}
	c.Assert(conf.Get("core", "a", &val), DeepEquals, &config.NoOptionError{SnapName: "core", Key: "a"})
	c.Assert(conf.Get("core", "foo", &val), IsNil)
	c.Check(val, DeepEquals, "bar")
}

func (s *applyCfgSuite) TestPlainCoreConfigGetMaybe(c *C) {
	conf := configcore.PlainCoreConfig(map[string]interface{}{"foo": "bar"})
	var val interface{}
	c.Assert(conf.GetMaybe("core", "a", &val), IsNil)
	c.Assert(val, IsNil)
	c.Assert(conf.Get("core", "foo", &val), IsNil)
	c.Check(val, DeepEquals, "bar")
}
