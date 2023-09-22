// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2021 Canonical Ltd
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
	"os"
	"path/filepath"
	"reflect"
	"testing"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/osutil/kcmdline"
	"github.com/snapcore/snapd/overlord/configstate/config"
	"github.com/snapcore/snapd/overlord/configstate/configcore"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/systemd"
	"github.com/snapcore/snapd/testutil"
)

func Test(t *testing.T) { TestingT(t) }

type mockConf struct {
	state   *state.State
	conf    map[string]interface{}
	changes map[string]interface{}
	err     error
	task    *state.Task
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

func (cfg *mockConf) Task() *state.Task {
	return cfg.task
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

func (cfg *mockConf) Commit() {}

// configcoreSuite is the base for all the configcore tests
type configcoreSuite struct {
	testutil.BaseTest

	state *state.State

	systemctlOutput   func(args ...string) []byte
	systemctlArgs     [][]string
	systemdSysctlArgs [][]string
}

var _ = Suite(&configcoreSuite{})

func (s *configcoreSuite) SetUpTest(c *C) {
	s.BaseTest.SetUpTest(c)

	dirs.SetRootDir(c.MkDir())
	s.AddCleanup(func() { dirs.SetRootDir("") })

	s.AddCleanup(osutil.MockMountInfo(""))

	s.systemctlOutput = func(args ...string) []byte {
		if args[0] == "show" {
			return []byte(fmt.Sprintf(`Type=notify
Id=%[1]s
Names=%[1]s
ActiveState=inactive
UnitFileState=enabled
NeedDaemonReload=no
`, args[len(args)-1]))
		}
		return []byte("ActiveState=inactive")
	}

	s.AddCleanup(systemd.MockSystemctl(func(args ...string) ([]byte, error) {
		s.systemctlArgs = append(s.systemctlArgs, args[:])
		return s.systemctlOutput(args...), nil
	}))
	s.systemctlArgs = nil
	s.AddCleanup(systemd.MockSystemdSysctl(func(args ...string) error {
		s.systemdSysctlArgs = append(s.systemdSysctlArgs, args[:])
		return nil
	}))
	s.systemdSysctlArgs = nil

	s.state = state.New(nil)

	restore := snap.MockSanitizePlugsSlots(func(snapInfo *snap.Info) {})
	s.AddCleanup(restore)

	// mock an empty cmdline since we check the cmdline to check whether we are
	// in install mode or uc20 run mode, etc. and we don't want to use the
	// host's proc/cmdline
	mockCmdline := filepath.Join(dirs.GlobalRootDir, "cmdline")
	err := os.WriteFile(mockCmdline, nil, 0644)
	c.Assert(err, IsNil)
	restore = kcmdline.MockProcCmdline(mockCmdline)
	s.AddCleanup(restore)
}

// runCfgSuite tests configcore.Run
type runCfgSuite struct {
	configcoreSuite
}

var _ = Suite(&runCfgSuite{})

type mockDev struct {
	mode    string
	classic bool
	kernel  string
	uc20    bool
}

func (d mockDev) RunMode() bool    { return d.mode == "" || d.mode == "run" }
func (d mockDev) Classic() bool    { return d.classic }
func (d mockDev) HasModeenv() bool { return d.uc20 }
func (d mockDev) Kernel() string {
	if d.Classic() {
		return ""
	}
	if d.kernel == "" {
		return "pc-kernel"
	}
	return d.kernel
}

var (
	coreDev    = mockDev{classic: false}
	classicDev = mockDev{classic: true}

	core20Dev = mockDev{classic: false, uc20: true}
)

// applyCfgSuite tests configcore.Apply()
type applyCfgSuite struct {
	testutil.BaseTest

	tmpDir string
}

var _ = Suite(&applyCfgSuite{})

func (s *applyCfgSuite) SetUpTest(c *C) {
	s.BaseTest.SetUpTest(c)

	s.tmpDir = c.MkDir()
	dirs.SetRootDir(s.tmpDir)
	s.AddCleanup(func() { dirs.SetRootDir("") })

	s.AddCleanup(osutil.MockMountInfo(""))
}

func (s *applyCfgSuite) TestEmptyRootDir(c *C) {
	err := configcore.FilesystemOnlyApply(coreDev, "", nil)
	c.Check(err, ErrorMatches, `internal error: root directory for configcore.FilesystemOnlyApply\(\) not set`)
}

func (s *applyCfgSuite) TestSmoke(c *C) {
	c.Assert(configcore.FilesystemOnlyApply(coreDev, s.tmpDir, map[string]interface{}{}), IsNil)
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

func (s *applyCfgSuite) TestNilHandleAddFSOnlyHandlerPanic(c *C) {
	c.Assert(func() { configcore.AddFSOnlyHandler(nil, nil, nil) },
		Panics, "cannot have nil handle with fsOnlyHandler")
}
