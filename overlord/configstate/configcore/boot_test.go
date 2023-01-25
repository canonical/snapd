// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2023 Canonical Ltd
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
	"io/ioutil"
	"os"
	"path/filepath"

	"github.com/snapcore/snapd/asserts"
	"github.com/snapcore/snapd/asserts/assertstest"
	"github.com/snapcore/snapd/asserts/sysdb"
	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/overlord"
	"github.com/snapcore/snapd/overlord/assertstate"
	"github.com/snapcore/snapd/overlord/assertstate/assertstatetest"
	"github.com/snapcore/snapd/overlord/auth"
	"github.com/snapcore/snapd/overlord/configstate/config"
	"github.com/snapcore/snapd/overlord/configstate/configcore"
	"github.com/snapcore/snapd/overlord/devicestate"
	"github.com/snapcore/snapd/overlord/devicestate/devicestatetest"
	"github.com/snapcore/snapd/overlord/hookstate"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/store/storetest"
	. "gopkg.in/check.v1"
)

type bootSuite struct {
	configcoreSuite
	storetest.Store

	StoreSigning *assertstest.StoreStack
	Brands       *assertstest.SigningAccounts
}

var _ = Suite(&bootSuite{})

var (
	brandPrivKey, _ = assertstest.GenerateKey(752)
)

func (s *bootSuite) SetUpTest(c *C) {
	s.configcoreSuite.SetUpTest(c)

	err := os.MkdirAll(filepath.Join(dirs.GlobalRootDir, "/etc/"), 0755)
	c.Assert(err, IsNil)
	mockEtcEnvironment := filepath.Join(dirs.GlobalRootDir, "/etc/environment")
	err = ioutil.WriteFile(mockEtcEnvironment, []byte{}, 0644)
	c.Assert(err, IsNil)

	o := overlord.Mock()
	// override state with the one from the overlord
	s.state = o.State()

	// we need a device context for this option
	hookMgr, err := hookstate.Manager(s.state, o.TaskRunner())
	c.Assert(err, IsNil)
	deviceMgr, err := devicestate.Manager(s.state, hookMgr, o.TaskRunner(), nil)
	c.Assert(err, IsNil)
	o.AddManager(deviceMgr)

	s.StoreSigning = assertstest.NewStoreStack("my-brand", nil)
	s.AddCleanup(sysdb.InjectTrusted(s.StoreSigning.Trusted))

	s.Brands = assertstest.NewSigningAccounts(s.StoreSigning)
	s.Brands.Register("my-brand", brandPrivKey, nil)

	db, err := asserts.OpenDatabase(&asserts.DatabaseConfig{
		Backstore:       asserts.NewMemoryBackstore(),
		Trusted:         s.StoreSigning.Trusted,
		OtherPredefined: s.StoreSigning.Generic,
	})
	s.state.Lock()
	assertstate.ReplaceDB(s.state, db)
	s.state.Unlock()

	c.Assert(err, IsNil)
}

func (s *bootSuite) mockModel(st *state.State, grade string) {
	s.state.Lock()
	defer s.state.Unlock()

	// model setup
	model := s.Brands.Model("my-brand", "pc", map[string]interface{}{
		"architecture": "amd64",
		"base":         "core20",
		"grade":        grade,
		"snaps": []interface{}{
			map[string]interface{}{
				"name":            "pc-kernel",
				"id":              "pYVQrBcKmBa0mZ4CCN7ExT6jH8rY1hza",
				"type":            "kernel",
				"default-channel": "20",
			},
			map[string]interface{}{
				"name":            "pc",
				"id":              "UqFziVZDHLSyO3TqSWgNBoAdHbLI4dAH",
				"type":            "gadget",
				"default-channel": "20",
			},
		},
	})

	assertstatetest.AddMany(s.state, model)

	devicestatetest.SetDevice(st, &auth.DeviceState{
		Brand:  model.BrandID(),
		Model:  model.Model(),
		Serial: "serialserial",
	})
}

func (s *bootSuite) testConfigureBootCmdlineHappy(c *C, option, modelGrade string) {
	s.mockModel(s.state, modelGrade)

	for _, cmdline := range []string{"param", "par=val param"} {
		s.state.Lock()
		ts := s.state.NewTask("hook-task", "system hook task")
		chg := s.state.NewChange("system-option", "...")
		chg.AddTask(ts)
		rt := configcore.NewRunTransaction(config.NewTransaction(s.state), ts)
		s.state.Unlock()

		rt.Set("core", option, cmdline)

		err := configcore.Run(coreDev, rt)
		c.Assert(err, IsNil)

		// Check that a new task has been added
		s.state.Lock()
		tasks := chg.Tasks()
		c.Assert(len(tasks), Equals, 2)
		c.Assert(tasks[0].Kind(), Equals, "hook-task")
		c.Assert(tasks[1].Kind(), Equals, "update-gadget-cmdline")
		var systemOption bool
		c.Assert(tasks[1].Get("system-option", &systemOption), IsNil)
		c.Assert(systemOption, Equals, true)
		s.state.Unlock()
	}
}

func (s *bootSuite) TestConfigureBootCmdlineHappy(c *C) {
	s.testConfigureBootCmdlineHappy(c, configcore.OptionBootCmdlineExtra, "dangerous")
	s.testConfigureBootCmdlineHappy(c, configcore.OptionBootCmdlineExtra, "signed")
	s.testConfigureBootCmdlineHappy(c, configcore.OptionBootDangerousCmdlineExtra, "dangerous")
}

func (s *bootSuite) TestConfigureBootCmdlineDangOnSigned(c *C) {
	s.mockModel(s.state, "signed")

	cmdline := "param1=val1"
	s.state.Lock()
	ts := s.state.NewTask("hook-task", "system hook task")
	chg := s.state.NewChange("system-option", "...")
	chg.AddTask(ts)
	rt := configcore.NewRunTransaction(config.NewTransaction(s.state), ts)
	s.state.Unlock()

	rt.Set("core", configcore.OptionBootDangerousCmdlineExtra, cmdline)

	err := configcore.Run(coreDev, rt)
	c.Assert(err, ErrorMatches, "cannot use system.boot.dangerous-cmdline-extra for non-dangerous model")
}
